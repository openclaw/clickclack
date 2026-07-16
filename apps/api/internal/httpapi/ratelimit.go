package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const rateLimiterMaxKeys = 4096

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
	nextGC time.Time
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
	if l.nextGC.IsZero() || !now.Before(l.nextGC) {
		l.sweep(cutoff)
		l.nextGC = now.Add(l.window)
	}
	if _, exists := l.hits[key]; !exists && len(l.hits) >= rateLimiterMaxKeys {
		// Keep memory bounded even when callers rotate through unbounded keys.
		// Evicting one arbitrary key avoids an O(n) oldest-entry scan on every
		// request after the cap is reached.
		for evicted := range l.hits {
			delete(l.hits, evicted)
			break
		}
	}
	kept := pruneAfter(l.hits[key], cutoff)
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
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

// clientIPKey derives the rate-limit key from the transport peer address. A
// loopback reverse proxy may supply one matching client IP in X-Real-IP and
// X-Forwarded-For; all other forwarding headers are ignored.
func clientIPKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil {
		return host
	}
	if remoteIP.IsLoopback() {
		if forwardedIP := loopbackProxyClientIP(r); forwardedIP != "" {
			return forwardedIP
		}
	}
	return remoteIP.String()
}

func loopbackProxyClientIP(r *http.Request) string {
	realValues := r.Header.Values("X-Real-IP")
	forwardedValues := r.Header.Values("X-Forwarded-For")
	if len(realValues) != 1 || len(forwardedValues) != 1 {
		return ""
	}
	realValue := strings.TrimSpace(realValues[0])
	forwardedValue := strings.TrimSpace(forwardedValues[0])
	if strings.Contains(forwardedValue, ",") {
		return ""
	}
	realIP := net.ParseIP(realValue)
	forwardedIP := net.ParseIP(forwardedValue)
	if realIP == nil || forwardedIP == nil || !realIP.Equal(forwardedIP) {
		return ""
	}
	return realIP.String()
}
