package http

import (
	"bufio"
	"net"
	stdlib "net/http"
	"strings"
	"testing"
	"time"
)

// A stream keeps going past the server's WriteTimeout.
func TestSSEStreamOutlivesWriteTimeout(t *testing.T) {
	srv := &stdlib.Server{WriteTimeout: 200 * time.Millisecond, Handler: stdlib.HandlerFunc(func(w stdlib.ResponseWriter, r *stdlib.Request) {
		c := &Context{Response: w, Request: r}
		_ = c.SSEStream(func(s *SSEWriter) error {
			for i := 0; i < 3; i++ {
				time.Sleep(150 * time.Millisecond)
				if err := s.Event("tick", "x"); err != nil {
					return err
				}
			}
			return s.Event("done", "ok")
		})
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	res, err := stdlib.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got strings.Builder
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		got.WriteString(sc.Text() + "\n")
	}
	if !strings.Contains(got.String(), "event: done") {
		t.Fatalf("stream was cut at the write deadline:\n%s", got.String())
	}
}

// A quiet stream sends keep-alive comments, so a proxy does not drop it.
func TestSSEStreamKeepAlive(t *testing.T) {
	old := SSEKeepAlive
	SSEKeepAlive = 50 * time.Millisecond
	defer func() { SSEKeepAlive = old }()
	srv := &stdlib.Server{Handler: stdlib.HandlerFunc(func(w stdlib.ResponseWriter, r *stdlib.Request) {
		c := &Context{Response: w, Request: r}
		_ = c.SSEStream(func(s *SSEWriter) error {
			time.Sleep(220 * time.Millisecond) // thinking, nothing to say
			return s.Event("done", "ok")
		})
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()
	res, err := stdlib.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got strings.Builder
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		got.WriteString(sc.Text() + "\n")
	}
	if n := strings.Count(got.String(), ": keep-alive"); n < 2 || !strings.Contains(got.String(), "event: done") {
		t.Fatalf("%d keep-alives:\n%s", n, got.String())
	}
}
