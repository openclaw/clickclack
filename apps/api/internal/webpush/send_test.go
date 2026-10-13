package webpush

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSendDeliversAnEncryptedPayload(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	var header http.Header
	var body []byte
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(relay.Close)

	sender := newTestSender(t, relay.Client())
	message := Message{Title: "Owner in #general", Body: "hello", Tag: "clickclack:msg_1", URL: "/app/wsp_1/chn_1"}
	if err := sender.Send(context.Background(), testSubscription(relay.URL+"/push/device", clientKey, authSecret), message); err != nil {
		t.Fatal(err)
	}
	if got := header.Get("Content-Encoding"); got != "aes128gcm" {
		t.Fatalf("content encoding %q", got)
	}
	if got := header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content type %q", got)
	}
	if got := header.Get("TTL"); got != strconv.Itoa(notificationTTL) {
		t.Fatalf("ttl %q", got)
	}
	if got := header.Get("Urgency"); got != "normal" {
		t.Fatalf("urgency %q", got)
	}
	authorization := header.Get("Authorization")
	if !strings.HasPrefix(authorization, "vapid t=") || !strings.Contains(authorization, ", k=") {
		t.Fatalf("authorization %q", authorization)
	}
	var delivered Message
	if err := json.Unmarshal(decryptRecord(t, clientKey, authSecret, body), &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered != message {
		t.Fatalf("delivered %#v, want %#v", delivered, message)
	}
}

func TestSendTruncatesTheBody(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	var body []byte
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(relay.Close)
	sender := newTestSender(t, relay.Client())
	long := strings.Repeat("e", 4000)
	if err := sender.Send(context.Background(), testSubscription(relay.URL+"/push/device", clientKey, authSecret), Message{Body: long}); err != nil {
		t.Fatal(err)
	}
	var delivered Message
	if err := json.Unmarshal(decryptRecord(t, clientKey, authSecret, body), &delivered); err != nil {
		t.Fatal(err)
	}
	if len([]rune(delivered.Body)) != MaxBodyRunes || !strings.HasSuffix(delivered.Body, "...") {
		t.Fatalf("unexpected truncation: %d runes", len([]rune(delivered.Body)))
	}
}

func TestSendClassifiesRelayResponses(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	status := http.StatusCreated
	retryAfterHeader := ""
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if retryAfterHeader != "" {
			w.Header().Set("Retry-After", retryAfterHeader)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(relay.Close)
	sender := newTestSender(t, relay.Client())
	subscription := testSubscription(relay.URL+"/push/device", clientKey, authSecret)

	for _, gone := range []int{http.StatusNotFound, http.StatusGone} {
		status = gone
		if err := sender.Send(context.Background(), subscription, Message{Body: "hi"}); !errors.Is(err, ErrSubscriptionGone) {
			t.Fatalf("status %d produced %v", gone, err)
		}
	}

	status = http.StatusServiceUnavailable
	retryAfterHeader = "120"
	err = sender.Send(context.Background(), subscription, Message{Body: "hi"})
	var relayErr *RelayError
	if !errors.As(err, &relayErr) {
		t.Fatalf("expected a relay error, got %v", err)
	}
	if relayErr.Status != http.StatusServiceUnavailable || relayErr.RetryAfter != 2*time.Minute {
		t.Fatalf("unexpected relay error %#v", relayErr)
	}
	if errors.Is(err, ErrSubscriptionGone) {
		t.Fatal("a temporary failure must never read as a dead subscription")
	}

	asked := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	retryAfterHeader = asked.Format(http.TimeFormat)
	err = sender.Send(context.Background(), subscription, Message{Body: "hi"})
	if !errors.As(err, &relayErr) {
		t.Fatalf("expected a relay error, got %v", err)
	}
	if retry := time.Now().Add(relayErr.RetryAfter); retry.Before(asked) {
		t.Fatalf("Retry-After %q became a retry at %s, before the relay asked", retryAfterHeader, retry.UTC().Format(time.RFC3339))
	}

	retryAfterHeader = "soon"
	err = sender.Send(context.Background(), subscription, Message{Body: "hi"})
	if !errors.As(err, &relayErr) || relayErr.RetryAfter != 0 {
		t.Fatalf("unexpected relay error %#v", relayErr)
	}
}

// TestRetryAfterReadsBothForms pins the header against a fixed clock: seconds
// or an HTTP-date in any of the three formats RFC 9110 requires a recipient to
// accept. A value that is past, empty, or neither form means no delay, and one
// too long to represent saturates rather than wrapping into a short retry.
func TestRetryAfterReadsBothForms(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)
	saturated := time.Duration(math.MaxInt64)
	for value, want := range map[string]time.Duration{
		"120":                              2 * time.Minute,
		" 120 ":                            2 * time.Minute,
		"Thu, 24 Sep 2026 12:10:00 GMT":    10 * time.Minute,
		"Thursday, 24-Sep-26 12:10:00 GMT": 10 * time.Minute,
		"Thu Sep 24 12:10:00 2026":         10 * time.Minute,
		"Fri, 24 Sep 2027 12:00:00 GMT":    365 * 24 * time.Hour,
		"Thu, 24 Sep 2026 11:50:00 GMT":    0,
		"Thu, 24 Sep 2026 12:00:00 GMT":    0,
		"0":                                0,
		"-5":                               0,
		"":                                 0,
		"soon":                             0,
		"12.5":                             0,
		"99999999999":                      saturated,
		"18446744074":                      saturated,
		"99999999999999999999":             saturated,
		"-99999999999999999999":            0,
	} {
		if got := retryAfter(value, now); got != want {
			t.Fatalf("retryAfter(%q) = %s, want %s", value, got, want)
		}
	}
}

// TestSendKeepsEndpointsOutOfErrors is the redaction guard: net/http wraps a
// transport failure in a *url.Error whose text is the full endpoint, and the
// endpoint path is the device's delivery secret.
func TestSendKeepsEndpointsOutOfErrors(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	release := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() {
		close(release)
		relay.Close()
	})
	sender := newTestSender(t, relay.Client())
	endpoint := relay.URL + "/push/AAAA-device-delivery-secret"
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = sender.Send(ctx, testSubscription(endpoint, clientKey, authSecret), Message{Body: "hi"})
	if err == nil {
		t.Fatal("expected the hanging relay to fail the send")
	}
	if strings.Contains(err.Error(), "device-delivery-secret") || strings.Contains(err.Error(), endpoint) {
		t.Fatalf("error leaked the endpoint: %v", err)
	}
	if !strings.Contains(err.Error(), RelayHost(endpoint)) {
		t.Fatalf("error should name the relay host: %v", err)
	}
}

func TestSendRejectsBadInput(t *testing.T) {
	t.Parallel()
	clientKey, err := ecdh.P256().NewPrivateKey(decodeVector(t, vectorClientPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	authSecret := decodeVector(t, vectorAuthSecret)
	sender := newTestSender(t, nil)
	valid := testSubscription("https://push.example.com/send/abc", clientKey, authSecret)
	for name, subscription := range map[string]Subscription{
		"bad p256dh": {Endpoint: valid.Endpoint, P256dh: "not base64!", Auth: valid.Auth},
		"bad auth":   {Endpoint: valid.Endpoint, P256dh: valid.P256dh, Auth: "not base64!"},
		"short auth": {Endpoint: valid.Endpoint, P256dh: valid.P256dh, Auth: "AAAA"},
		"bad scheme": {Endpoint: "://nonsense", P256dh: valid.P256dh, Auth: valid.Auth},
	} {
		if err := sender.Send(context.Background(), subscription, Message{Body: "hi"}); err == nil {
			t.Fatalf("expected %s to fail", name)
		}
	}
	var unconfigured *Sender
	if err := unconfigured.Send(context.Background(), valid, Message{}); err == nil {
		t.Fatal("expected an unconfigured sender to fail")
	}
	if err := (&Sender{}).Send(context.Background(), valid, Message{}); err == nil {
		t.Fatal("expected a keyless sender to fail")
	}
}

func TestRelayHostFallsBackWhenTheEndpointIsUnusable(t *testing.T) {
	t.Parallel()
	if got := RelayHost("://nonsense"); got != "unknown" {
		t.Fatalf("relay host %q", got)
	}
}

func newTestSender(t *testing.T, client *http.Client) *Sender {
	t.Helper()
	publicKey, privateKey, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &Sender{PublicKey: publicKey, PrivateKey: privateKey, Subject: "https://chat.example.com", Client: client}
}

func testSubscription(endpoint string, clientKey *ecdh.PrivateKey, authSecret []byte) Subscription {
	return Subscription{
		Endpoint: endpoint,
		P256dh:   encodeVector(clientKey.PublicKey().Bytes()),
		Auth:     encodeVector(authSecret),
	}
}
