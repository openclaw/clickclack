// Package webpush sends RFC 8030 Web Push messages to the browser push
// services: RFC 8291 payload encryption, RFC 8292 application server
// identification, and nothing else. It knows about a VAPID key pair, a
// subscription, and an HTTP client, so it can be lifted out of the server
// unchanged.
package webpush

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxBodyRunes bounds the message text a notification carries. Lock
	// screens truncate long bodies anyway, and the payload has to stay inside
	// one encrypted record.
	MaxBodyRunes = 240
	// notificationTTL asks the push service to hold an undelivered message for
	// a day, which covers a phone that is off overnight.
	notificationTTL = 24 * 60 * 60
)

// ErrSubscriptionGone reports that the push service has permanently rejected
// the subscription. The caller deletes the stored row; no other failure means
// the device is gone.
var ErrSubscriptionGone = errors.New("push subscription is gone")

// Subscription is the durable half of a browser PushSubscription.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Message is the payload a service worker receives.
type Message struct {
	UserID string `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Tag    string `json:"tag"`
	URL    string `json:"url"`
}

// Sender holds the application server identity shared by every push.
type Sender struct {
	PublicKey  string
	PrivateKey string
	Subject    string
	Client     *http.Client
}

// RelayError is a push service refusal that says nothing about whether the
// subscription is still good: rate limits, outages, and anything else the
// service reports with a status code. The caller backs off.
type RelayError struct {
	Host       string
	Status     int
	RetryAfter time.Duration
}

func (e *RelayError) Error() string {
	return fmt.Sprintf("push service %s answered %d", e.Host, e.Status)
}

// Send encrypts one message for a subscription and posts it to the push
// service.
func (s *Sender) Send(ctx context.Context, subscription Subscription, message Message) error {
	if s == nil || strings.TrimSpace(s.PublicKey) == "" || strings.TrimSpace(s.PrivateKey) == "" {
		return errors.New("web push keys are not configured")
	}
	clientPublicKey, err := DecodeKey(subscription.P256dh)
	if err != nil {
		return fmt.Errorf("subscription p256dh: %w", err)
	}
	authSecret, err := DecodeKey(subscription.Auth)
	if err != nil {
		return fmt.Errorf("subscription auth: %w", err)
	}
	message.Body = truncateBody(message.Body)
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	senderKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt, err := newSalt()
	if err != nil {
		return err
	}
	body, err := encryptRecord(clientPublicKey, authSecret, payload, salt, senderKey)
	if err != nil {
		return err
	}
	authorization, err := s.authorizationHeader(subscription.Endpoint, time.Now())
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.Endpoint, bytes.NewReader(body))
	if err != nil {
		return withoutEndpoint(err)
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("TTL", strconv.Itoa(notificationTTL))
	request.Header.Set("Urgency", "normal")
	response, err := s.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("push service %s: %w", RelayHost(subscription.Endpoint), withoutEndpoint(err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return ErrSubscriptionGone
	case response.StatusCode >= 300:
		return &RelayError{
			Host:       RelayHost(subscription.Endpoint),
			Status:     response.StatusCode,
			RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now()),
		}
	}
	return nil
}

func (s *Sender) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

// ValidateSubscriptionKeys rejects client keys that cannot produce a payload:
// a p256dh that is not a point on P-256, or an auth secret of the wrong
// length. Registration calls it so a device can never be stored in a state the
// delivery worker would choke on.
func ValidateSubscriptionKeys(p256dh, auth string) error {
	publicKey, err := DecodeKey(p256dh)
	if err != nil {
		return fmt.Errorf("p256dh must be base64url: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(publicKey); err != nil {
		return errors.New("p256dh must be an uncompressed P-256 public key")
	}
	secret, err := DecodeKey(auth)
	if err != nil {
		return fmt.Errorf("auth must be base64url: %w", err)
	}
	if len(secret) != authSecretLength {
		return fmt.Errorf("auth must be %d bytes", authSecretLength)
	}
	return nil
}

// RelayHost reports the push service host for a subscription endpoint. It is
// the only part of an endpoint that is safe to log: the path carries the
// device's delivery secret.
func RelayHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "unknown"
	}
	return parsed.Host
}

// withoutEndpoint strips the request URL that net/http wraps around transport
// failures. Without this the first timeout writes a delivery secret into the
// log.
func withoutEndpoint(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// retryAfter reads the header in both forms RFC 9110 allows: a number of
// seconds, or an HTTP-date, which is measured from now. A date already past,
// or a value that is neither, leaves the caller's own backoff in charge. A
// delay too long to count in nanoseconds saturates instead of wrapping into
// a short one, so the caller's cap applies rather than its shortest backoff.
func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(math.MaxInt64/time.Second) {
			return math.MaxInt64
		}
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return max(date.Sub(now), 0)
}

func truncateBody(body string) string {
	body = strings.TrimSpace(body)
	runes := []rune(body)
	if len(runes) <= MaxBodyRunes {
		return body
	}
	return string(runes[:MaxBodyRunes-3]) + "..."
}
