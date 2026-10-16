package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompletionStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		status  int
		aborted bool
	}{
		{"empty", func(http.ResponseWriter, *http.Request) {}, 200, false},
		{"implicit", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }, 200, false},
		{"explicit", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403); w.WriteHeader(500) }, 403, false},
		{"flush", func(w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Error(err)
			}
		}, 200, false},
		{"panic", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }, 0, true},
		{"partial panic", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); panic(http.ErrAbortHandler) }, 202, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			h := LoggerMiddleware(slog.New(slog.NewJSONHandler(&output, nil)))(tc.handler)
			func() {
				defer func() {
					if v := recover(); v != nil && v != http.ErrAbortHandler {
						panic(v)
					}
				}()
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			}()
			var record struct {
				Status   int
				Aborted  bool
				Duration int64
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Status != tc.status || record.Aborted != tc.aborted || record.Duration <= 0 {
				t.Fatalf("%+v", record)
			}
		})
	}
}

func TestLoggerPreservesStreamingAndHijacking(t *testing.T) {
	for _, hijack := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "hijack"}[hijack], func(t *testing.T) {
			logged := make(chan struct{})
			var output bytes.Buffer
			h := LoggerMiddleware(slog.New(slog.NewJSONHandler(&output, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				controller := http.NewResponseController(w)
				if err := controller.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
					t.Error(err)
					return
				}
				if hijack {
					conn, rw, err := controller.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					_, err = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
					if err == nil {
						err = rw.Flush()
					}
					if err != nil {
						t.Error(err)
					}
					return
				}
				w.Write([]byte("ok"))
				if err := controller.Flush(); err != nil {
					t.Error(err)
				}
			}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r); close(logged) }))
			defer server.Close()
			client := server.Client()
			client.Timeout = 3 * time.Second
			response, err := client.Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil || string(data) != "ok" {
				t.Fatalf("%s %v", data, err)
			}
			select {
			case <-logged:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not finish")
			}
			var record struct {
				Status   int
				Hijacked bool
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			want := 200
			if hijack {
				want = 0
			}
			if record.Status != want || record.Hijacked != hijack {
				t.Fatalf("%+v", record)
			}
		})
	}
}

func TestLoggerPreservesInformationalResponses(t *testing.T) {
	var output bytes.Buffer
	finished := make(chan struct{})
	h := LoggerMiddleware(slog.New(slog.NewJSONHandler(&output, nil)))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(103)
		w.WriteHeader(201)
		w.Write([]byte("created"))
	}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r); close(finished) }))
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	data, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	<-finished
	if !bytes.Contains(data, []byte("103 Early Hints")) || !bytes.Contains(data, []byte("201 Created")) || !bytes.Contains(output.Bytes(), []byte(`"status":201`)) {
		t.Fatalf("responses: %s log: %s", data, output.String())
	}
}

func TestLoggedResponseInformationalAndController(t *testing.T) {
	w := &loggedResponse{ResponseWriter: httptest.NewRecorder()}
	w.WriteHeader(103)
	if w.status != 0 {
		t.Fatal(w.status)
	}
	w.WriteHeader(201)
	if w.status != 201 {
		t.Fatal(w.status)
	}
	if w.Unwrap() == nil {
		t.Fatal("lost underlying writer")
	}
	if _, _, err := w.Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal(err)
	}
	if err := w.Push("/asset", nil); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal(err)
	}
}
