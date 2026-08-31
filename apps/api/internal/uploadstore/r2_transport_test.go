package uploadstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type r2RoundTripFunc func(*http.Request) (*http.Response, error)

func (f r2RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setR2DefaultTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func r2TestStore(t *testing.T, endpoint string) *R2 {
	t.Helper()
	store, err := NewR2(R2Config{Endpoint: endpoint, Bucket: "synthetic-bucket", AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestR2HeaderTimeoutAllMethods(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				written := make(chan struct{})
				var wrote, firstByte int
				setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
					trace := httptrace.ContextClientTrace(r.Context())
					trace.WroteRequest(httptrace.WroteRequestInfo{})
					trace.GotFirstResponseByte()
					close(written)
					<-r.Context().Done()
					return nil, context.Cause(r.Context())
				}))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
					WroteRequest:         func(httptrace.WroteRequestInfo) { wrote++ },
					GotFirstResponseByte: func() { firstByte++ },
				})
				store := r2TestStore(t, "http://synthetic.invalid")
				done := make(chan error, 1)
				go func() {
					var err error
					switch method {
					case http.MethodPut:
						_, err = store.Save(ctx, strings.NewReader("synthetic"), SaveOptions{})
					case http.MethodDelete:
						err = store.Delete(ctx, "object")
					case http.MethodGet:
						err = store.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx), Object{Path: "object"})
					}
					done <- err
				}()
				<-written
				time.Sleep(defaultR2ResponseHeaderTimeout + time.Second)
				synctest.Wait()
				select {
				case err := <-done:
					var timeout interface{ Timeout() bool }
					if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &timeout) || !timeout.Timeout() {
						t.Fatalf("header timeout lost deadline semantics: %v", err)
					}
				default:
					t.Fatal("complete-header wait remained blocked after its deadline")
				}
				if wrote != 1 || firstByte != 1 {
					t.Fatalf("existing trace hooks were lost: wrote=%d firstByte=%d", wrote, firstByte)
				}
			})
		})
	}
}

type pacedR2Body struct{ io.ReadCloser }

func (b pacedR2Body) Read(p []byte) (int, error) {
	time.Sleep(time.Second)
	return b.ReadCloser.Read(p[:1])
}

func TestR2HeaderNetworkLifecycle(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(base.CloseIdleConnections)
	var calls atomic.Int32
	setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method == http.MethodPut {
			r = r.Clone(r.Context())
			r.Body = pacedR2Body{r.Body}
		}
		return base.RoundTrip(r)
	}))
	t.Run("complete headers", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nX-Synthetic: incomplete")
			_ = rw.Flush()
			_, _ = io.Copy(io.Discard, conn)
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
		defer cancel()
		started := time.Now()
		err := r2TestStore(t, server.URL).Delete(ctx, "object")
		elapsed := time.Since(started)
		if !errors.Is(err, context.DeadlineExceeded) || elapsed < 29*time.Second || elapsed > 35*time.Second {
			t.Fatalf("complete-header watchdog: elapsed=%s error=%v", elapsed, err)
		}
		t.Logf("partial headers cancelled after %s; configured wrapper calls=%d", elapsed, calls.Load())
	})
	t.Run("progressing PUT", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil || n != 35 {
				t.Errorf("progressing PUT: bytes=%d error=%v", n, err)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		started := time.Now()
		saved, err := r2TestStore(t, server.URL).Save(t.Context(), strings.NewReader(strings.Repeat("x", 35)), SaveOptions{})
		if err != nil || saved.ByteSize != 35 || time.Since(started) <= defaultR2ResponseHeaderTimeout {
			t.Fatalf("progressing PUT failed: saved=%+v elapsed=%s error=%v", saved, time.Since(started), err)
		}
		if calls.Load() < 2 {
			t.Fatal("configured default wrapper was bypassed")
		}
		t.Logf("progressing PUT completed after %s with %d bytes", time.Since(started), saved.ByteSize)
	})
}

func TestR2HeaderRetriesAndBodyLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requestContext context.Context
		var trace *httptrace.ClientTrace
		var connections atomic.Int32
		setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestContext = r.Context()
			trace = httptrace.ContextClientTrace(r.Context())
			trace.GetConn("synthetic.invalid")
			time.Sleep(31 * time.Second)
			trace.WroteRequest(httptrace.WroteRequestInfo{})
			time.Sleep(29 * time.Second)
			trace.GetConn("synthetic.invalid")
			time.Sleep(31 * time.Second)
			trace.WroteRequest(httptrace.WroteRequestInfo{Err: errors.New("synthetic failed write")})
			time.Sleep(31 * time.Second)
			if err := context.Cause(r.Context()); err != nil {
				t.Fatalf("upload/retry/write failure armed a header timer: %v", err)
			}
			trace.WroteRequest(httptrace.WroteRequestInfo{})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("body"))}, nil
		}))
		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GetConn: func(string) { connections.Add(1) }})
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://synthetic.invalid", nil)
		resp, err := defaultR2HTTPClient().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		// A transport may report hooks after headers, including from concurrent goroutines.
		var hooks sync.WaitGroup
		for range 10 {
			hooks.Go(func() { trace.WroteRequest(httptrace.WroteRequestInfo{}); trace.GetConn("late") })
		}
		hooks.Wait()
		time.Sleep(31 * time.Second)
		if err := context.Cause(requestContext); err != nil {
			t.Fatalf("late hook cancelled response body: %v", err)
		}
		if connections.Load() != 12 {
			t.Fatalf("GetConn hooks were not composed: %d", connections.Load())
		}
		p := make([]byte, 1)
		if n, err := resp.Body.Read(p); n != 1 || err != nil {
			t.Fatalf("successful response was cancelled: n=%d err=%v", n, err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(context.Cause(requestContext), context.Canceled) {
			t.Fatal("Close did not release the request context")
		}
		trace.WroteRequest(httptrace.WroteRequestInfo{})
		time.Sleep(31 * time.Second)
	})
}

func TestR2HeaderExpiredCallbackCannotCancelBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		lifetime := &r2ResponseLifetime{cancel: cancel}
		lifetime.wroteRequest(httptrace.WroteRequestInfo{})
		lifetime.mu.Lock()
		// Let the callback become runnable while the header-to-body transition
		// holds the lock, reproducing Stop(false) without a scheduler race.
		time.Sleep(defaultR2ResponseHeaderTimeout)
		if lifetime.timer.Stop() {
			t.Fatal("expected the callback to have expired")
		}
		lifetime.phase = r2Body
		lifetime.stopTimerLocked()
		lifetime.mu.Unlock()
		synctest.Wait()
		if err := context.Cause(ctx); err != nil {
			t.Fatalf("expired header callback cancelled body phase: %v", err)
		}
		lifetime.close()
	})
}

func TestR2ResponseCleanup(t *testing.T) {
	for _, outcome := range []string{"error", "nil response", "nil empty body", "nil nonempty body", "EOF", "parent cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			var requestContext context.Context
			failure := errors.New("synthetic transport failure")
			setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestContext = r.Context()
				switch outcome {
				case "error":
					return nil, failure
				case "nil response":
					return nil, nil
				case "nil empty body":
					return &http.Response{StatusCode: 200}, nil
				case "nil nonempty body":
					return &http.Response{StatusCode: 200, ContentLength: 1}, nil
				default:
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("body"))}, nil
				}
			}))
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://synthetic.invalid", nil)
			resp, err := defaultR2HTTPClient().Do(req)
			switch outcome {
			case "error", "nil response", "nil nonempty body":
				if err == nil {
					t.Fatal("http.Client validation/transport error was masked")
				}
				if outcome == "error" && !errors.Is(err, failure) {
					t.Fatal(err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if outcome == "parent cancellation" {
					cancel(failure)
					if !errors.Is(context.Cause(requestContext), failure) {
						t.Fatal("parent cancellation cause was lost")
					}
				} else if _, err := io.ReadAll(resp.Body); err != nil {
					t.Fatal(err)
				}
			}
			if context.Cause(requestContext) == nil {
				t.Fatal("terminal response did not release its context")
			}
		})
	}
}

func TestR2CustomClientOwnsRequestPolicy(t *testing.T) {
	setR2DefaultTransport(t, r2RoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("explicit client fell through to the default transport")
		return nil, errors.New("unexpected default request")
	}))
	redirectError := errors.New("synthetic redirect policy")
	client := &http.Client{
		Timeout: time.Second,
		Transport: r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"http://synthetic.invalid/redirect"}}, Body: http.NoBody, Request: r}, nil
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return redirectError },
	}
	store, err := NewR2(R2Config{Endpoint: "http://synthetic.invalid", Bucket: "bucket", AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if store.httpClient != client || client.Timeout != time.Second {
		t.Fatal("explicit client was changed")
	}
	if err := store.Delete(t.Context(), "object"); !errors.Is(err, redirectError) {
		t.Fatalf("custom redirect policy was lost: %v", err)
	}
}
