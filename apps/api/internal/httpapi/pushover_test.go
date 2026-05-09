package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestPushoverNotifierPostsForm(t *testing.T) {
	var body string
	notifier := NewPushoverNotifier("app-token")
	notifier.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != pushoverMessagesURL {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("unexpected content type %q", got)
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":1}`)),
		}, nil
	})}
	if err := notifier.Notify(context.Background(), PushNotification{
		RecipientKey: "user-key",
		Title:        "ClickClack",
		Message:      "Owner: hello",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"token=app-token", "user=user-key", "title=ClickClack", "message=Owner%3A+hello"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected form to contain %q, got %q", want, body)
		}
	}
}

func TestPushoverNotifierReportsFailures(t *testing.T) {
	notifier := NewPushoverNotifier("app-token")
	notifier.Client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":0,"errors":["bad user"]}`)),
		}, nil
	})}
	if err := notifier.Notify(context.Background(), PushNotification{RecipientKey: "user-key", Message: "hello"}); err == nil {
		t.Fatal("expected pushover failure")
	}
}

func TestPushoverNotifierValidatesInputsAndFailures(t *testing.T) {
	var nilNotifier *PushoverNotifier
	if err := nilNotifier.Notify(context.Background(), PushNotification{RecipientKey: "user-key"}); err == nil {
		t.Fatal("expected nil notifier error")
	}
	if err := NewPushoverNotifier(" ").Notify(context.Background(), PushNotification{RecipientKey: "user-key"}); err == nil {
		t.Fatal("expected missing token error")
	}
	if err := NewPushoverNotifier("app-token").Notify(context.Background(), PushNotification{RecipientKey: " "}); err == nil {
		t.Fatal("expected missing user key error")
	}

	notifier := NewPushoverNotifier("app-token")
	notifier.Client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	if err := notifier.Notify(context.Background(), PushNotification{RecipientKey: "user-key"}); err == nil {
		t.Fatal("expected transport error")
	}

	notifier.Client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":0}`)),
		}, nil
	})}
	if err := notifier.Notify(context.Background(), PushNotification{RecipientKey: "user-key"}); err == nil {
		t.Fatal("expected status failure")
	}
}

func TestNotificationText(t *testing.T) {
	parent := "msg_parent"
	blank := notificationBody(store.Message{AuthorID: "usr_1", Author: &store.User{DisplayName: "  "}})
	if blank != "usr_1 sent a message" {
		t.Fatalf("unexpected blank body: %q", blank)
	}
	long := notificationBody(store.Message{AuthorID: "usr_1", Author: &store.User{DisplayName: "Peter"}, Body: strings.Repeat("x", 501)})
	if len([]rune(strings.TrimPrefix(long, "Peter: "))) != 503 || !strings.HasSuffix(long, "...") {
		t.Fatalf("unexpected truncated body: %q", long)
	}
	if got := notificationTitle(store.Message{DirectConversationID: "dm_1"}); got != "ClickClack DM" {
		t.Fatalf("unexpected DM title: %q", got)
	}
	if got := notificationTitle(store.Message{ParentMessageID: &parent}); got != "ClickClack thread" {
		t.Fatalf("unexpected thread title: %q", got)
	}
	if got := notificationTitle(store.Message{}); got != "ClickClack" {
		t.Fatalf("unexpected channel title: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
