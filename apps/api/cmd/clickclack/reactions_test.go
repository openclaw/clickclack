package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestReactionCommandsUseExistingAPIAndAuth(t *testing.T) {
	tests := []struct {
		name        string
		action      string
		method      string
		messageID   string
		emoji       string
		token       string
		userID      string
		wantAuth    string
		wantDevUser string
	}{
		{
			name:      "add with bearer token",
			action:    "add",
			method:    http.MethodPost,
			messageID: "msg/add",
			emoji:     "👍",
			token:     "ses_test",
			wantAuth:  "Bearer ses_test",
		},
		{
			name:        "remove with development user",
			action:      "remove",
			method:      http.MethodDelete,
			messageID:   "msg_remove",
			emoji:       ":claw:/blue",
			userID:      "usr_test",
			wantDevUser: "usr_test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if r.Method != tc.method {
					t.Fatalf("method = %s, want %s", r.Method, tc.method)
				}
				wantPath := "/api/messages/" + tc.messageID + "/reactions"
				if tc.action == "remove" {
					wantPath += "/" + tc.emoji
				}
				if r.URL.Path != wantPath {
					t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
				}
				wantEscapedPath := "/api/messages/" + url.PathEscape(tc.messageID) + "/reactions"
				if tc.action == "remove" {
					wantEscapedPath += "/" + url.PathEscape(tc.emoji)
				}
				if r.URL.EscapedPath() != wantEscapedPath {
					t.Fatalf("escaped path = %q, want %q", r.URL.EscapedPath(), wantEscapedPath)
				}
				if r.Header.Get("Authorization") != tc.wantAuth {
					t.Fatalf("Authorization = %q, want %q", r.Header.Get("Authorization"), tc.wantAuth)
				}
				if r.Header.Get("X-ClickClack-User") != tc.wantDevUser {
					t.Fatalf("X-ClickClack-User = %q, want %q", r.Header.Get("X-ClickClack-User"), tc.wantDevUser)
				}
				if tc.action == "add" {
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["emoji"] != tc.emoji {
						t.Fatalf("emoji = %q, want %q", body["emoji"], tc.emoji)
					}
				} else if r.Header.Get("Content-Type") != "" {
					t.Fatalf("DELETE Content-Type = %q, want empty", r.Header.Get("Content-Type"))
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(reactionMutationResponse{
					Event: store.Event{ID: "evt_1", Type: "reaction." + pastTenseAction(tc.action)},
					Reactions: []store.ReactionSummary{{
						Emoji:       tc.emoji,
						Count:       1,
						ReactedByMe: tc.action == "add",
					}},
				})
			}))
			t.Cleanup(server.Close)

			c := apiClient{
				opts: clientOptions{
					Server: server.URL,
					Token:  tc.token,
					UserID: tc.userID,
					JSON:   true,
				},
				http: server.Client(),
			}
			output := captureStdout(t, func() error {
				return c.reactions([]string{tc.action, tc.messageID, tc.emoji})
			})
			var result reactionMutationResponse
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("decode JSON output: %v\n%s", err, output)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(output), &fields); err != nil {
				t.Fatalf("decode JSON fields: %v\n%s", err, output)
			}
			if len(fields) != 2 || fields["event"] == nil || fields["reactions"] == nil {
				t.Fatalf("JSON output keys = %v, want exactly event and reactions", fields)
			}
			if result.Event.ID != "evt_1" || len(result.Reactions) != 1 || result.Reactions[0].Emoji != tc.emoji {
				t.Fatalf("unexpected JSON output: %#v", result)
			}
			if requestCount != 1 {
				t.Fatalf("request count = %d, want 1", requestCount)
			}
		})
	}
}

func TestClientDispatchesReactionCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/messages/msg_1/reactions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reactionMutationResponse{})
	}))
	t.Cleanup(server.Close)

	output := captureStdout(t, func() error {
		return client([]string{
			"--server", server.URL,
			"--user", "usr_test",
			"reactions", "add", "msg_1", "👍", "--json",
		})
	})
	var result reactionMutationResponse
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, output)
	}
}

func TestReactionHumanOutputDoesNotEchoUntrustedEmoji(t *testing.T) {
	messageID := "msg_\n\u202eunsafe"
	inputs := []string{
		"ordinary",
		"line\nbreak",
		"carriage\rreturn",
		"escape\x1b[31m",
		"bidi\u202ereversed",
		"👍",
	}
	for index, emoji := range inputs {
		t.Run(pastTenseAction("add")+"_"+string(rune('a'+index)), func(t *testing.T) {
			var receivedEmoji string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				receivedEmoji = body["emoji"]
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(reactionMutationResponse{
					Reactions: []store.ReactionSummary{{Emoji: emoji, Count: 1, ReactedByMe: true}},
				})
			}))
			t.Cleanup(server.Close)

			c := apiClient{opts: clientOptions{Server: server.URL}, http: server.Client()}
			output := captureStdout(t, func() error {
				return c.reactions([]string{"add", messageID, emoji})
			})
			if receivedEmoji != emoji {
				t.Fatalf("API emoji = %q, want original %q", receivedEmoji, emoji)
			}
			wantOutput := "reaction added on " + url.PathEscape(messageID) + "\n"
			if output != wantOutput {
				t.Fatalf("human output = %q", output)
			}
			if strings.ContainsAny(output[:len(output)-1], "\r\n\x1b") || strings.ContainsRune(output, '\u202e') {
				t.Fatalf("human output contains unsafe terminal controls: %q", output)
			}
		})
	}
}

func TestReactionCommandsValidateBeforeNetworkRequest(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		opts clientOptions
		args []string
		want string
	}{
		{name: "missing command", args: nil, want: "usage:"},
		{name: "unknown command", args: []string{"toggle", "msg_1", "👍"}, want: `unknown reactions command "toggle"`},
		{name: "missing emoji", args: []string{"add", "msg_1"}, want: "usage:"},
		{name: "extra argument", args: []string{"remove", "msg_1", "👍", "extra"}, want: "usage:"},
		{
			name: "plain output",
			opts: clientOptions{Plain: true},
			args: []string{"add", "msg_1", "👍"},
			want: "--plain is not supported",
		},
		{
			name: "post-operand plain output",
			args: []string{"remove", "msg_1", "👍", "--plain"},
			want: "--plain is not supported",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := apiClient{opts: clientOptions{Server: server.URL, Plain: tc.opts.Plain}, http: server.Client()}
			err := c.reactions(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
	if requestCount != 0 {
		t.Fatalf("request count = %d, want 0", requestCount)
	}
}

func TestReactionCommandsUseCurrentClientErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		want       string
	}{
		{name: "API error", statusCode: http.StatusForbidden, response: `{"error":"forbidden"}`, want: "403 Forbidden: forbidden"},
		{name: "malformed success", statusCode: http.StatusOK, response: `{`, want: "unexpected end of JSON input"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = io.WriteString(w, tc.response)
			}))
			t.Cleanup(server.Close)
			c := apiClient{opts: clientOptions{Server: server.URL}, http: server.Client()}
			err := c.reactions([]string{"add", "msg_1", "👍"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}

	c := apiClient{
		opts: clientOptions{Server: "http://clickclack.invalid"},
		http: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		})},
	}
	err := c.reactions([]string{"remove", "msg_1", "👍"})
	if err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("network error = %v", err)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	oldStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = oldStdout })
	if err := fn(); err != nil {
		_ = write.Close()
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func pastTenseAction(action string) string {
	if action == "add" {
		return "added"
	}
	return "removed"
}
