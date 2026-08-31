package uploadstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestR2ErrorBodiesHaveReadIdleBound(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			cancelled := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("synthetic upstream error"))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(cancelled)
			}))
			defer origin.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			store := r2TestStore(t, origin.URL)
			started := time.Now()
			var err error
			switch method {
			case http.MethodPut:
				_, err = store.Save(ctx, strings.NewReader("synthetic"), SaveOptions{})
			case http.MethodDelete:
				err = store.Delete(ctx, "object")
			case http.MethodGet:
				err = store.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx), Object{Path: "object"})
			}
			elapsed := time.Since(started)
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "synthetic upstream error") || elapsed < 29*time.Second || elapsed > 35*time.Second {
				t.Fatalf("stalled %s diagnostic was not bounded with its cause: elapsed=%s error=%v", method, elapsed, err)
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("upstream request was not cancelled")
			}
			t.Logf("%s stalled error body cancelled after %s", method, elapsed)
		})
	}
}

type r2BlockedBody struct{ ctx context.Context }

func (b r2BlockedBody) Read(p []byte) (int, error) {
	<-b.ctx.Done()
	return copy(p, "tail"), context.Cause(b.ctx)
}

func (b r2BlockedBody) Close() error {
	if b.ctx.Err() == nil {
		return errors.New("body closed before request cancellation")
	}
	return nil
}

func TestR2BodyReadCancellation(t *testing.T) {
	for _, cause := range []string{"idle", "parent", "close"} {
		t.Run(cause, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: r2BlockedBody{r.Context()}}, nil
				}))
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://synthetic.invalid", nil)
				resp, err := defaultR2HTTPClient().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				type result struct {
					body []byte
					err  error
				}
				done := make(chan result, 1)
				go func() { b, err := io.ReadAll(resp.Body); done <- result{b, err} }()
				synctest.Wait()
				expected := context.DeadlineExceeded
				switch cause {
				case "idle":
					time.Sleep(31 * time.Second)
				case "parent":
					expected = errors.New("synthetic parent cancellation")
					cancel(expected)
				case "close":
					expected = context.Canceled
					if err := resp.Body.Close(); err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()
				select {
				case got := <-done:
					if string(got.body) != "tail" || !errors.Is(got.err, expected) {
						t.Fatalf("lost bytes or cancellation cause: %+v", got)
					}
				default:
					t.Fatal("request cancellation did not interrupt Read")
				}
			})
		})
	}
}

type r2ReadFunc func([]byte) (int, error)

func (f r2ReadFunc) Read(p []byte) (int, error) { return f(p) }
func (r2ReadFunc) Close() error                 { return nil }
func (r2ReadFunc) WriteTo(io.Writer) (int64, error) {
	return 0, errors.New("underlying WriterTo bypassed timed Read")
}

type r2WriteFunc func([]byte) (int, error)

func (f r2WriteFunc) Write(p []byte) (int, error) { return f(p) }

func TestR2BodyProgressAndDownstreamBackpressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var upstream context.Context
		reads := 0
		setR2DefaultTransport(t, r2RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			upstream = r.Context()
			body := r2ReadFunc(func(p []byte) (int, error) {
				if reads == 2 {
					return 0, io.EOF
				}
				reads++
				time.Sleep(20 * time.Second)
				if err := context.Cause(upstream); err != nil {
					return 0, err
				}
				return copy(p, "x"), nil
			})
			return &http.Response{StatusCode: 200, Body: body}, nil
		}))
		resp, err := defaultR2HTTPClient().Get("http://synthetic.invalid")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		n, err := io.Copy(r2WriteFunc(func(p []byte) (int, error) {
			time.Sleep(31 * time.Second)
			if err := context.Cause(upstream); err != nil {
				return 0, err
			}
			return len(p), nil
		}), resp.Body)
		if err != nil || n != 2 {
			t.Fatalf("progress/backpressure was charged to upstream idle or Read was bypassed: bytes=%d error=%v", n, err)
		}
	})
}

func TestR2ExpiredBodyCallbackCannotCancelNextRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(t.Context())
		lifetime := &r2ResponseLifetime{cancel: cancel, phase: r2Body}
		lifetime.mu.Lock()
		lifetime.armTimerLocked(defaultR2ResponseBodyIdleTimeout)
		time.Sleep(defaultR2ResponseBodyIdleTimeout)
		if lifetime.timer.Stop() {
			t.Fatal("expected the old read callback to have expired")
		}
		lifetime.armTimerLocked(defaultR2ResponseBodyIdleTimeout)
		lifetime.mu.Unlock()
		synctest.Wait()
		if err := context.Cause(ctx); err != nil {
			t.Fatalf("expired callback cancelled a later Read: %v", err)
		}
		time.Sleep(defaultR2ResponseBodyIdleTimeout)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			t.Fatal("new read deadline did not remain active")
		}
		lifetime.close()
	})
}

func TestR2ErrorDiagnosticKeepsByteLimit(t *testing.T) {
	err := responseError("synthetic", &http.Response{Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 512) + "do not include"))})
	if err.Error() != "synthetic: 503 Service Unavailable: "+strings.Repeat("x", 512) {
		t.Fatalf("diagnostic byte limit changed: %v", err)
	}
}
