package uploadstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

const defaultR2ResponseBodyIdleTimeout = 30 * time.Second

// r2Transport preserves the configured default transport and its trace hooks.
// After-write timing requires a transport that emits Go's HTTP lifecycle hooks.
type r2Transport struct{}

func (r2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	lifetime := &r2ResponseLifetime{cancel: cancel}
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn:      func(string) { lifetime.retry() },
		WroteRequest: lifetime.wroteRequest,
	})
	resp, err := http.DefaultTransport.RoundTrip(req.WithContext(ctx))
	lifetime.headersComplete()
	if err != nil || resp == nil || resp.Body == nil {
		// Let http.Client retain its validation and empty-body normalization.
		lifetime.close()
		return resp, err
	}
	resp.Body = &r2ResponseBody{body: resp.Body, lifetime: lifetime}
	return resp, nil
}

type r2ResponsePhase uint8

const (
	r2Headers r2ResponsePhase = iota
	r2Body
	r2Closed
)

type r2ResponseLifetime struct {
	mu         sync.Mutex
	phase      r2ResponsePhase
	generation uint64
	timer      *time.Timer
	cancel     context.CancelCauseFunc
}

func (l *r2ResponseLifetime) stopTimerLocked() {
	l.generation++
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
}

func (l *r2ResponseLifetime) wroteRequest(info httptrace.WroteRequestInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != r2Headers || info.Err != nil {
		return
	}
	l.armTimerLocked(defaultR2ResponseHeaderTimeout)
}

func (l *r2ResponseLifetime) armTimerLocked(timeout time.Duration) {
	l.stopTimerLocked()
	generation := l.generation
	phase := l.phase
	l.timer = time.AfterFunc(timeout, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		// Stop does not join an already-started callback. Check and cancel
		// under the same lock so it cannot cancel a later attempt or phase.
		if l.phase == phase && l.generation == generation {
			l.cancel(context.DeadlineExceeded)
		}
	})
}

func (l *r2ResponseLifetime) retry() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase == r2Headers {
		l.stopTimerLocked()
	}
}

func (l *r2ResponseLifetime) headersComplete() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.phase = r2Body
	l.stopTimerLocked()
}

func (l *r2ResponseLifetime) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.phase = r2Closed
	l.stopTimerLocked()
	l.cancel(nil)
}

type r2ResponseBody struct {
	body     io.ReadCloser
	lifetime *r2ResponseLifetime
}

func (b *r2ResponseBody) Read(p []byte) (int, error) {
	l := b.lifetime
	l.mu.Lock()
	if l.phase == r2Body {
		l.armTimerLocked(defaultR2ResponseBodyIdleTimeout)
	}
	l.mu.Unlock()
	n, err := b.body.Read(p)
	l.mu.Lock()
	// End the upstream wait before io.Copy writes to a potentially slow client.
	l.stopTimerLocked()
	if err != nil {
		l.phase = r2Closed
		l.cancel(nil)
	}
	l.mu.Unlock()
	return n, err
}

func (b *r2ResponseBody) Close() error {
	b.lifetime.close()
	return b.body.Close()
}
