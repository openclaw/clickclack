package httpapi

import (
	"net"
	"net/http"
	"slices"
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
	hits   map[string][]*rateLimitAttempt
	nextGC time.Time
}

type rateLimitAttempt struct {
	at time.Time
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		limit:  limit,
		window: window,
		nowFn:  time.Now,
		hits:   map[string][]*rateLimitAttempt{},
	}
}

func (l *slidingWindowLimiter) allow(key string) bool {
	return l.reserve(key) != nil
}

// reserve charges in-flight work atomically; callers may refund non-failures.
func (l *slidingWindowLimiter) reserve(key string) *rateLimitAttempt {
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
		return nil
	}
	attempt := &rateLimitAttempt{at: now}
	l.hits[key] = append(kept, attempt)
	return attempt
}

func (l *slidingWindowLimiter) refund(key string, attempt *rateLimitAttempt) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Identity survives equal timestamps and expired or evicted buckets.
	entries := l.hits[key]
	if i := slices.Index(entries, attempt); i >= 0 {
		entries = slices.Delete(entries, i, i+1)
		if len(entries) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = entries
		}
	}
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

func pruneAfter(entries []*rateLimitAttempt, cutoff time.Time) []*rateLimitAttempt {
	return slices.DeleteFunc(entries, func(attempt *rateLimitAttempt) bool {
		return !attempt.at.After(cutoff)
	})
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
