package store

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxPushSubscriptionsPerUser caps how many devices one account keeps.
	// The oldest is evicted, so reinstalling a browser repeatedly cannot grow
	// the table without bound.
	MaxPushSubscriptionsPerUser = 10
	// maxPushEndpointLength bounds the push service URL. Real endpoints are a
	// few hundred bytes; this leaves room without accepting a payload.
	maxPushEndpointLength = 2048
	// maxPushSubscriptionKeyLength bounds the base64url client keys, which are
	// 65 and 16 bytes before encoding.
	maxPushSubscriptionKeyLength = 256
	// maxPushUserAgentRunes bounds the device label shown in settings. It is a
	// label the client chooses, never the raw user agent string.
	maxPushUserAgentRunes = 200
	// MaxPushRetryDelay caps the backoff after a push service refuses a
	// delivery. A device that has been unreachable for a day is retried daily,
	// not abandoned: a row is removed when the service says the subscription
	// is gone, or by PrunePushSubscriptions, never for one failure.
	MaxPushRetryDelay = 24 * time.Hour
	// PushFailingPruneAfter is how long a push service may refuse a device
	// before its row is pruned. An outage is shorter than a week.
	PushFailingPruneAfter = 7 * 24 * time.Hour
	// PushFailingPruneRefusedWithin is how recent the latest refusal of that
	// week must be. Retries ride on messages, so two refusals in one short
	// outage and then a quiet week are not a week of refusals: the run must
	// still be refusing at its end.
	PushFailingPruneRefusedWithin = 24 * time.Hour
	// PushFailingPruneMinFailures is how many refusals that week must hold,
	// so one refusal never removes a device.
	PushFailingPruneMinFailures = 2
	// PushRetiredKeyPruneAfter is how long a device registered under a
	// retired key may go without registering again before its row is pruned.
	// Opening the app replaces its subscription; a month without that means
	// nobody is opening it.
	PushRetiredKeyPruneAfter = 30 * 24 * time.Hour
)

// pushRetryLadder is the backoff after consecutive delivery failures. A relay
// outage must not cost users their subscriptions, so this replaces any
// strike-out rule.
var pushRetryLadder = []time.Duration{
	time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	6 * time.Hour,
	MaxPushRetryDelay,
}

var pushSubscriptionKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_=-]+$`)

var (
	// ErrPushSessionEnded reports that the session a device was registered
	// under has been signed out, revoked, or expired since the push was queued.
	ErrPushSessionEnded = errors.New("push subscription session has ended")
	// ErrPushSubscriptionBackingOff reports that the push service asked for a
	// pause that the next attempt has not waited out yet.
	ErrPushSubscriptionBackingOff = errors.New("push subscription is backing off")
	// ErrPushSubscriptionKeyRetired reports that the device registered under
	// an application server key the server no longer signs with, so its push
	// service refuses every push.
	ErrPushSubscriptionKeyRetired = errors.New("push subscription is under a retired key")
)

// PushSubscription is the summary of a registered device. It deliberately
// omits the endpoint and the client keys: those are delivery secrets and never
// leave the server.
type PushSubscription struct {
	ID            string  `json:"id"`
	UserID        string  `json:"-"`
	UserAgent     string  `json:"user_agent"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	LastSuccessAt *string `json:"last_success_at,omitempty"`
	FailureCount  int64   `json:"failure_count"`
	// EndpointKey identifies the device without the endpoint: see
	// PushEndpointKey. It is compared on the server and never serialized.
	EndpointKey string `json:"-"`
	// KeyID names the application server key the device registered under;
	// empty for a device registered before the server recorded it.
	KeyID string `json:"-"`
}

// PushEndpointKey is the unpadded base64url SHA-256 of a subscription
// endpoint. A browser computes the same digest of the subscription it holds,
// so the server can say whether that browser is one of an account's devices
// without either side sending the endpoint.
func PushEndpointKey(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// PushSubscriptionInput is one browser PushSubscription being registered.
// SessionToken binds the device to the session that registered it, so signing
// out stops that device receiving message text. A registration without one is
// refused unless DevelopmentActor says the caller is the local development
// fallback, the only way to be signed in without a session. KeyID names the
// application server key the server signs with now. A new device records it;
// the same endpoint registered again keeps the one it was first registered
// under, because it is the same browser subscription.
type PushSubscriptionInput struct {
	UserID           string
	Endpoint         string
	P256dh           string
	Auth             string
	UserAgent        string
	SessionToken     string
	DevelopmentActor bool
	KeyID            string
}

// ErrPushSubscriptionNeedsSession refuses a device registration that has no
// session to follow. Such a row could never be stopped by signing out.
var ErrPushSubscriptionNeedsSession = errors.New("push registration needs a signed-in session")

// PushSubscriptionTarget carries what a delivery needs, and the key the
// device registered under so a sender can skip one it no longer signs with.
type PushSubscriptionTarget struct {
	Endpoint string
	P256dh   string
	Auth     string
	KeyID    string
}

// NormalizePushSubscriptionInput validates a subscription the way both stores
// need it: a well formed endpoint URL, client keys that look like base64url,
// and a short device label. Which endpoint hosts the server is willing to call
// is a transport question, decided before the store is reached.
func NormalizePushSubscriptionInput(input PushSubscriptionInput) (PushSubscriptionInput, error) {
	if input.SessionToken == "" && !input.DevelopmentActor {
		return PushSubscriptionInput{}, ErrPushSubscriptionNeedsSession
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if endpoint == "" {
		return PushSubscriptionInput{}, errors.New("endpoint is required")
	}
	if len(endpoint) > maxPushEndpointLength {
		return PushSubscriptionInput{}, errors.New("endpoint is too long")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return PushSubscriptionInput{}, errors.New("endpoint must be an absolute push service URL")
	}
	p256dh, err := normalizePushSubscriptionKey(input.P256dh, "p256dh")
	if err != nil {
		return PushSubscriptionInput{}, err
	}
	auth, err := normalizePushSubscriptionKey(input.Auth, "auth")
	if err != nil {
		return PushSubscriptionInput{}, err
	}
	userAgent := strings.TrimSpace(input.UserAgent)
	if runes := []rune(userAgent); len(runes) > maxPushUserAgentRunes {
		userAgent = string(runes[:maxPushUserAgentRunes])
	}
	if !utf8.ValidString(userAgent) || strings.IndexByte(userAgent, 0) >= 0 {
		return PushSubscriptionInput{}, errors.New("user_agent must be valid UTF-8")
	}
	return PushSubscriptionInput{
		UserID:           input.UserID,
		Endpoint:         endpoint,
		P256dh:           p256dh,
		Auth:             auth,
		UserAgent:        userAgent,
		SessionToken:     input.SessionToken,
		DevelopmentActor: input.DevelopmentActor,
		KeyID:            input.KeyID,
	}, nil
}

// PushRetryDelay reports how long to wait before the next delivery attempt
// after failureCount consecutive failures, honoring a longer Retry-After from
// the push service.
func PushRetryDelay(failureCount int64, retryAfter time.Duration) time.Duration {
	index := failureCount - 1
	if index < 0 {
		index = 0
	}
	if index >= int64(len(pushRetryLadder)) {
		index = int64(len(pushRetryLadder)) - 1
	}
	delay := pushRetryLadder[index]
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > MaxPushRetryDelay {
		delay = MaxPushRetryDelay
	}
	return delay
}

// PushSubscriptionReady reports whether a stored subscription may be used for
// a delivery now: its backoff has elapsed, and the session that registered it
// is still live. An empty session timestamp means the row was registered
// without a session, which only development authentication can do.
func PushSubscriptionReady(nextAttemptAt, sessionExpiresAt string, now time.Time) bool {
	if nextAttemptAt != "" {
		next, err := time.Parse(time.RFC3339Nano, nextAttemptAt)
		if err != nil || now.Before(next) {
			return false
		}
	}
	if sessionExpiresAt == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, sessionExpiresAt)
	return err == nil && now.Before(expiresAt)
}

// PushKeyRetired reports whether a device registered under keyID can no
// longer receive from a server signing with currentKeyID. An empty id on
// either side is unknown, never retired: a row from before the key was
// recorded is not condemned on a guess.
func PushKeyRetired(keyID, currentKeyID string) bool {
	return keyID != "" && currentKeyID != "" && keyID != currentKeyID
}

// PushDeliveryState is a stored subscription as the delivery worker re-reads
// it immediately before sending: the device's own row, and the session it was
// registered under, which is absent when that session no longer exists.
type PushDeliveryState struct {
	UserID           string
	NextAttemptAt    string
	KeyID            string
	SessionTokenHash string
	SessionUserID    string
	SessionExpiresAt string
	SessionRevokedAt string
}

// CheckPushDelivery applies the rules recipient selection uses a second time,
// at send time, because a push can sit in the delivery queue after the
// session behind it ends. It answers ErrPushSessionEnded when the registering
// session is gone, revoked, expired, or belongs to someone else,
// ErrPushSubscriptionKeyRetired when the device registered under a key other
// than currentKeyID, and ErrPushSubscriptionBackingOff when the backoff has
// not elapsed.
func CheckPushDelivery(state PushDeliveryState, currentKeyID string, now time.Time) error {
	if state.SessionTokenHash != "" {
		if state.SessionUserID == "" || state.SessionUserID != state.UserID || state.SessionRevokedAt != "" {
			return ErrPushSessionEnded
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, state.SessionExpiresAt)
		if err != nil || !now.Before(expiresAt) {
			return ErrPushSessionEnded
		}
	}
	if PushKeyRetired(state.KeyID, currentKeyID) {
		return ErrPushSubscriptionKeyRetired
	}
	if state.NextAttemptAt != "" {
		next, err := time.Parse(time.RFC3339Nano, state.NextAttemptAt)
		if err != nil || now.Before(next) {
			return ErrPushSubscriptionBackingOff
		}
	}
	return nil
}

// PushPruneCutoffs are the instants PrunePushSubscriptions compares stored
// timestamps against, as text. Stored timestamps are RFC 3339 with trailing
// fraction zeros trimmed, so each cutoff is written with all nine fraction
// digits: a stored time then sorts before a cutoff only when it is earlier.
// The reverse can fail inside the cutoff's own second, which only leaves a
// removal to the next pass and never makes one early.
type PushPruneCutoffs struct {
	// FailingBefore and LastFailureSince: a device refused since before
	// FailingBefore, most recently at or after LastFailureSince, at least
	// PushFailingPruneMinFailures times, is pruned.
	FailingBefore string
	// LastFailureSince is compared the other way, a stored time at or after
	// it, so it is the next whole second after the instant, written with no
	// fraction: a stored time sorts at or after it only when it is no
	// earlier than the instant.
	LastFailureSince string
	// RetiredKeyUpdatedBefore: a device under a retired key last written
	// before this is pruned.
	RetiredKeyUpdatedBefore string
	// SessionExpiredBy: a session expiring at or before this has ended.
	SessionExpiredBy string
}

// NewPushPruneCutoffs derives the cutoffs from one reading of the clock.
func NewPushPruneCutoffs(now time.Time) PushPruneCutoffs {
	format := func(instant time.Time) string {
		return instant.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
	}
	refusedSince := now.Add(-PushFailingPruneRefusedWithin).UTC().Truncate(time.Second).Add(time.Second)
	return PushPruneCutoffs{
		FailingBefore:           format(now.Add(-PushFailingPruneAfter)),
		LastFailureSince:        refusedSince.Format(time.RFC3339),
		RetiredKeyUpdatedBefore: format(now.Add(-PushRetiredKeyPruneAfter)),
		SessionExpiredBy:        format(now),
	}
}

// PushPruneResult counts the devices one PrunePushSubscriptions pass removed,
// by the rule that removed each. A device matching more than one is counted
// once, under the first.
type PushPruneResult struct {
	Failing      int64
	RetiredKey   int64
	SessionEnded int64
}

// Total is every device the pass removed.
func (r PushPruneResult) Total() int64 {
	return r.Failing + r.RetiredKey + r.SessionEnded
}

func normalizePushSubscriptionKey(value, name string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", errors.New(name + " is required")
	}
	if len(key) > maxPushSubscriptionKeyLength || !pushSubscriptionKeyPattern.MatchString(key) {
		return "", errors.New(name + " must be a base64url key")
	}
	return key, nil
}
