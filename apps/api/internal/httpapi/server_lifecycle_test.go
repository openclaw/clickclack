package httpapi

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListenAndServeDrainsActiveRequest(t *testing.T) {
	t.Parallel()
	for _, finish := range []bool{true, false} {
		name := "finished response"
		if !finish {
			name = "drain deadline"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := listener.Addr().String()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			handlerDone := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- ListenAndServe(ctx, addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(handlerDone)
					close(started)
					select {
					case <-release:
						_, _ = io.WriteString(w, "completed")
					case <-r.Context().Done():
					}
				}))
			}()
			var conn net.Conn
			deadline := time.Now().Add(5 * time.Second)
			for {
				conn, err = net.DialTimeout("tcp", addr, 100*time.Millisecond)
				if err == nil {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("server failed before request: %v", err)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatalf("server never accepted connections: %v", err)
				}
				time.Sleep(time.Millisecond)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request handler never started")
			}
			cancel()
			select {
			case err := <-done:
				t.Fatalf("server returned before its active request finished: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if finish {
				release <- struct{}{}
				response, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || response.StatusCode != http.StatusOK || string(body) != "completed" {
					t.Fatalf("active response was not preserved: status=%s body=%q err=%v", response.Status, body, err)
				}
			}
			select {
			case err := <-done:
				if finish && err != nil || !finish && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("unexpected drain result: %v", err)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("server did not finish draining")
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("server left an active request after its drain deadline")
			}
		})
	}
}

func TestListenAndServeReturnsBindError(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()
	err = ListenAndServe(context.Background(), addr, http.NotFoundHandler())
	if err == nil || !strings.Contains(err.Error(), addr) {
		t.Fatalf("expected the listen error for %s, got %v", addr, err)
	}
}
