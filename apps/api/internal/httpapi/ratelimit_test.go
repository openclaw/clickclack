package httpapi

import (
	"strconv"
	"testing"
	"time"
)

func TestLimiterRefundUsesAttemptIdentity(t *testing.T) {
	limiter := newSlidingWindowLimiter(2, time.Minute)
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	limiter.nowFn = func() time.Time { return now }
	first, second := limiter.reserve("account"), limiter.reserve("account")
	if first == nil || second == nil || limiter.allow("account") {
		t.Fatal("expected exactly two reservations")
	}
	limiter.refund("account", first)
	if !limiter.allow("account") {
		t.Fatal("expected a refunded slot to be available")
	}
	limiter.refund("account", first)
	if limiter.allow("account") {
		t.Fatal("repeated refund removed a different attempt with the same timestamp")
	}
	limiter.refund("account", second)
	if !limiter.allow("account") || limiter.allow("account") {
		t.Fatal("expected the second refund to release only its own slot")
	}
}

func TestLimiterRefundCannotRemoveReplacementAfterExpiry(t *testing.T) {
	limiter := newSlidingWindowLimiter(1, time.Minute)
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	limiter.nowFn = func() time.Time { return now }
	old := limiter.reserve("account")
	now = now.Add(time.Minute)
	replacement := limiter.reserve("account")
	if old == nil || replacement == nil {
		t.Fatal("expected a fresh budget when the window expires")
	}
	limiter.refund("account", old)
	if limiter.allow("account") {
		t.Fatal("expired reservation refunded a replacement attempt")
	}
	limiter.refund("account", replacement)
	if !limiter.allow("account") {
		t.Fatal("replacement reservation could not be refunded")
	}
}

func TestLimiterRefundCannotRemoveReplacementAfterEviction(t *testing.T) {
	limiter := newSlidingWindowLimiter(1, time.Minute)
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	limiter.nowFn = func() time.Time { return now }
	reservations := make(map[string]*rateLimitAttempt, rateLimiterMaxKeys)
	for i := 0; i < rateLimiterMaxKeys; i++ {
		key := strconv.Itoa(i)
		reservations[key] = limiter.reserve(key)
	}
	if !limiter.allow("overflow") || len(limiter.hits) != rateLimiterMaxKeys {
		t.Fatal("expected admission with bounded key state")
	}
	for key, old := range reservations {
		if _, exists := limiter.hits[key]; exists {
			continue
		}
		replacement := limiter.reserve(key)
		if old == nil || replacement == nil {
			t.Fatal("expected the evicted key to receive a fresh budget")
		}
		limiter.refund(key, old)
		if limiter.allow(key) {
			t.Fatal("evicted reservation refunded a replacement attempt")
		}
		limiter.refund(key, replacement)
		if !limiter.allow(key) || len(limiter.hits) > rateLimiterMaxKeys {
			t.Fatal("expected only the replacement to refund within the key bound")
		}
		return
	}
	t.Fatal("expected an eviction at the key bound")
}
