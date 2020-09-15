package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/fhs/gompd/mpd"
	"github.com/hajimehoshi/oto"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mattn/go-tty"
)

func play(uri string, ch chan map[string]string) error {
	resp, err := http.Get(uri)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		var buf [8192]byte
		for {
			n, err := resp.Body.Read(buf[:])
			if err != nil {
				break
			}
			_, err = pw.Write(buf[:n])
			if err != nil {
				break
			}
		}
	}()

	st, err := oggvorbis.NewReader(bufio.NewReader(pr))
	if err != nil {
		return err
	}

	info := map[string]string{}
	for _, c := range st.CommentHeader().Comments {
		pos := strings.Index(c, "=")
		if pos == -1 {
			continue
		}
		info[c[:pos]] = c[pos+1:]
	}
	ch <- info

	bufferSize := 4000
	numBytes := bufferSize * 2

	context, err := oto.NewContext(
		st.SampleRate(),
		st.Channels(),
		2,
		numBytes)
	if err != nil {
		return err
	}
	defer context.Close()

	player := context.NewPlayer()
	defer player.Close()

	samples := make([]float32, bufferSize)

	buf := make([]byte, numBytes)
	for {
		n, err := st.Read(samples)
		if err != nil {
			return io.EOF
		}

		for i, val := range samples[:n] {
			if val < -1 {
				val = -1
			}
			if val > +1 {
				val = +1
			}
			valInt16 := int16(val * (1<<15 - 1))
			low := byte(valInt16)
			high := byte(valInt16 >> 8)
			buf[i*2+0] = low
			buf[i*2+1] = high
		}
		player.Write(buf[:n*2])
	}
}

func main() {
	var addr, stream string
	flag.StringVar(&stream, "stream", "", "Stream URL")
	flag.StringVar(&addr, "addr", "127.0.0.1:6600", "Server address")
	flag.Parse()

	if stream != "" {
		ch := make(chan map[string]string)
		go func() {
			for {
				err := play(stream, ch)
				if err != nil {
					if errors.Is(err, io.EOF) {
						continue
					}
					break
				}
			}
		}()

		go func() {
			for ii := range ch {
				fmt.Printf("%s : %s\n", ii["ARTIST"], ii["TITLE"])
			}
		}()
	}

	client, err := mpd.Dial("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	t, err := tty.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	for {
		r, err := t.ReadRune()
		if err != nil {
			log.Fatal(err)
		}
		switch r {
		case 'j':
			client.Next()
		case 'k':
			client.Previous()
		case ' ':
			attr, _ := client.Status()
			if attr != nil && attr["state"] == "play" {
				client.Pause(true)
			} else {
				client.Pause(false)
			}
		}
	}
}
