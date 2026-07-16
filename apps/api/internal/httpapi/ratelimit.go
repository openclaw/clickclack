package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const rateLimiterSweepThreshold = 4096

// slidingWindowLimiter is a small in-memory per-key rate limiter for
// unauthenticated endpoints. It is intentionally process-local: multi-node
// deployments get a per-node budget, which is sufficient to blunt
// brute-force attempts against high-entropy codes.
type slidingWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	nowFn  func() time.Time
	hits   map[string][]time.Time
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		limit:  limit,
		window: window,
		nowFn:  time.Now,
		hits:   map[string][]time.Time{},
	}
}

func (l *slidingWindowLimiter) allow(key string) bool {
	now := l.nowFn()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := pruneAfter(l.hits[key], cutoff)
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	if len(l.hits) > rateLimiterSweepThreshold {
		l.sweep(cutoff)
	}
	return true
}

func (l *slidingWindowLimiter) sweep(cutoff time.Time) {
	for key, entries := range l.hits {
		kept := pruneAfter(entries, cutoff)
		if len(kept) == 0 {
			delete(l.hits, key)
			continue
		}
		l.hits[key] = kept
	}
}

func pruneAfter(entries []time.Time, cutoff time.Time) []time.Time {
	kept := entries[:0]
	for _, t := range entries {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

// clientIPKey derives the rate-limit key from the transport peer address.
// Forwarding headers are deliberately not trusted here: honoring them would
// let clients rotate keys freely.
func clientIPKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
