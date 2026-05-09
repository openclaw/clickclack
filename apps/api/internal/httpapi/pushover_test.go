package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
