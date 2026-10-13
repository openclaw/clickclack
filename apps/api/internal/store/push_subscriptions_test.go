package store

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestCheckPushDelivery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	live := PushDeliveryState{
		UserID:           "usr_1",
		SessionTokenHash: "hash",
		SessionUserID:    "usr_1",
		SessionExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano),
	}
	for name, testCase := range map[string]struct {
		change func(*PushDeliveryState)
		want   error
	}{
		"live session":       {func(*PushDeliveryState) {}, nil},
		"development row":    {func(state *PushDeliveryState) { *state = PushDeliveryState{UserID: "usr_1"} }, nil},
		"backoff elapsed":    {func(state *PushDeliveryState) { state.NextAttemptAt = now.Add(-time.Minute).Format(time.RFC3339Nano) }, nil},
		"session missing":    {func(state *PushDeliveryState) { state.SessionUserID, state.SessionExpiresAt = "", "" }, ErrPushSessionEnded},
		"session of another": {func(state *PushDeliveryState) { state.SessionUserID = "usr_2" }, ErrPushSessionEnded},
		"session revoked":    {func(state *PushDeliveryState) { state.SessionRevokedAt = now.Format(time.RFC3339Nano) }, ErrPushSessionEnded},
		"session expired":    {func(state *PushDeliveryState) { state.SessionExpiresAt = now.Format(time.RFC3339Nano) }, ErrPushSessionEnded},
		"unreadable expiry":  {func(state *PushDeliveryState) { state.SessionExpiresAt = "soon" }, ErrPushSessionEnded},
		"backing off":        {func(state *PushDeliveryState) { state.NextAttemptAt = now.Add(time.Minute).Format(time.RFC3339Nano) }, ErrPushSubscriptionBackingOff},
		"unreadable backoff": {func(state *PushDeliveryState) { state.NextAttemptAt = "later" }, ErrPushSubscriptionBackingOff},
		"ended and backed off": {func(state *PushDeliveryState) {
			state.SessionRevokedAt = now.Format(time.RFC3339Nano)
			state.NextAttemptAt = now.Add(time.Minute).Format(time.RFC3339Nano)
		}, ErrPushSessionEnded},
		"current key":      {func(state *PushDeliveryState) { state.KeyID = "key-now" }, nil},
		"key not recorded": {func(state *PushDeliveryState) { state.KeyID = "" }, nil},
		"retired key":      {func(state *PushDeliveryState) { state.KeyID = "key-before" }, ErrPushSubscriptionKeyRetired},
		"retired and backed off": {func(state *PushDeliveryState) {
			state.KeyID = "key-before"
			state.NextAttemptAt = now.Add(time.Minute).Format(time.RFC3339Nano)
		}, ErrPushSubscriptionKeyRetired},
		"ended and retired": {func(state *PushDeliveryState) {
			state.KeyID = "key-before"
			state.SessionRevokedAt = now.Format(time.RFC3339Nano)
		}, ErrPushSessionEnded},
	} {
		state := live
		testCase.change(&state)
		if got := CheckPushDelivery(state, "key-now", now); !errors.Is(got, testCase.want) || (testCase.want == nil && got != nil) {
			t.Fatalf("%s: got %v, want %v", name, got, testCase.want)
		}
	}
}

func TestNormalizePushSubscriptionInputRequiresASession(t *testing.T) {
	t.Parallel()
	input := PushSubscriptionInput{
		UserID:   "usr_1",
		Endpoint: "https://push.example.com/send/1",
		P256dh:   "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
	}
	if _, err := NormalizePushSubscriptionInput(input); !errors.Is(err, ErrPushSubscriptionNeedsSession) {
		t.Fatalf("a sessionless registration must be refused: %v", err)
	}
	input.SessionToken = "session"
	if _, err := NormalizePushSubscriptionInput(input); err != nil {
		t.Fatalf("a session binds the device: %v", err)
	}
	input.SessionToken = ""
	input.DevelopmentActor = true
	if _, err := NormalizePushSubscriptionInput(input); err != nil {
		t.Fatalf("the development identity has no session to bind: %v", err)
	}
}

func TestPushKeyRetiredNeedsBothKeysToDiffer(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		keyID, currentKeyID string
		want                bool
	}{
		"same key":            {"key-now", "key-now", false},
		"another key":         {"key-before", "key-now", true},
		"key not recorded":    {"", "key-now", false},
		"current key unknown": {"key-before", "", false},
		"neither known":       {"", "", false},
	} {
		if got := PushKeyRetired(testCase.keyID, testCase.currentKeyID); got != testCase.want {
			t.Fatalf("%s: retired is %v, want %v", name, got, testCase.want)
		}
	}
}

// A relay's Retry-After can only lengthen the ladder, and never past the cap,
// however long the delay it asked for.
func TestPushRetryDelayHonorsALongerRetryAfterUpToTheCap(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		failures   int64
		retryAfter time.Duration
		want       time.Duration
	}{
		{1, 0, time.Minute},
		{1, 10 * time.Minute, 10 * time.Minute},
		{3, 10 * time.Minute, 30 * time.Minute},
		{1, 365 * 24 * time.Hour, MaxPushRetryDelay},
		{1, time.Duration(math.MaxInt64), MaxPushRetryDelay},
		{99, 0, MaxPushRetryDelay},
	} {
		if got := PushRetryDelay(testCase.failures, testCase.retryAfter); got != testCase.want {
			t.Fatalf("PushRetryDelay(%d, %s) = %s, want %s", testCase.failures, testCase.retryAfter, got, testCase.want)
		}
	}
}

// Stored timestamps drop trailing fraction zeros, so text order and time order
// part inside one second. Each "before" cutoff carries all nine digits, and a
// stored time in the cutoff's own second must never sort before it unless it
// is earlier, or a sweep would remove a device a moment early.
func TestPushPruneCutoffsNeverSortALaterTimeFirst(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 22, 12, 0, 0, 300_000_000, time.UTC)
	cutoffs := NewPushPruneCutoffs(now)
	if cutoffs.SessionExpiredBy != "2026-09-22T12:00:00.300000000Z" {
		t.Fatalf("session cutoff is %q", cutoffs.SessionExpiredBy)
	}
	if cutoffs.FailingBefore != "2026-09-15T12:00:00.300000000Z" {
		t.Fatalf("failing cutoff is %q", cutoffs.FailingBefore)
	}
	if cutoffs.RetiredKeyUpdatedBefore != "2026-08-23T12:00:00.300000000Z" {
		t.Fatalf("retired key cutoff is %q", cutoffs.RetiredKeyUpdatedBefore)
	}
	if cutoffs.LastFailureSince != "2026-09-21T12:00:01Z" {
		t.Fatalf("latest refusal cutoff is %q", cutoffs.LastFailureSince)
	}
	// The latest refusal is compared the other way: a stored time must never
	// sort at or after that cutoff unless it is no earlier than a day ago.
	dayAgo := now.Add(-PushFailingPruneRefusedWithin)
	for _, offset := range []time.Duration{
		-time.Second, -time.Nanosecond, 0, time.Nanosecond, 699 * time.Millisecond, 700 * time.Millisecond,
		time.Second, 1700 * time.Millisecond, 2 * time.Second, time.Hour,
	} {
		refused := dayAgo.Add(offset)
		stored := refused.Format(time.RFC3339Nano)
		if stored >= cutoffs.LastFailureSince && refused.Before(dayAgo) {
			t.Fatalf("%q sorts at or after the cutoff %q but is earlier than a day ago", stored, cutoffs.LastFailureSince)
		}
		if offset >= 2*time.Second && stored < cutoffs.LastFailureSince {
			t.Fatalf("%q is two seconds or more inside the day but sorts before %q", stored, cutoffs.LastFailureSince)
		}
	}
	for _, offset := range []time.Duration{
		-2 * time.Second, -time.Second, -300 * time.Millisecond, -time.Millisecond, -time.Nanosecond,
		0, time.Nanosecond, 100 * time.Millisecond, 700 * time.Millisecond, time.Second,
	} {
		stored := now.Add(offset).Format(time.RFC3339Nano)
		if stored < cutoffs.SessionExpiredBy && !now.Add(offset).Before(now) {
			t.Fatalf("%q sorts before the cutoff %q but is not earlier", stored, cutoffs.SessionExpiredBy)
		}
		if offset <= -time.Second && stored >= cutoffs.SessionExpiredBy {
			t.Fatalf("%q is a second or more earlier but does not sort before %q", stored, cutoffs.SessionExpiredBy)
		}
	}
	if (PushPruneResult{Failing: 1, RetiredKey: 2, SessionEnded: 3}).Total() != 6 {
		t.Fatal("the total must count every rule")
	}
}
