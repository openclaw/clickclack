package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestRunCanaryProvesGatewayAndQuotedBotRoundTrip(t *testing.T) {
	const correlationID = "fakeco-test-001"
	quoted := "msg_request"
	var mu sync.Mutex
	var correlations []string
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		correlations = append(correlations, r.Header.Get("X-Correlation-ID"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/gateway-healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.URL.Path == "/api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user": store.User{ID: "usr_alice", Kind: "human", DisplayName: "Alice"}})
		case r.URL.Path == "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{{ID: "wsp_fakeco", Slug: "fakeco", Name: "FakeCo"}}})
		case r.URL.Path == "/api/workspaces/wsp_fakeco/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{{ID: "chn_canary", WorkspaceID: "wsp_fakeco", Name: "e2e-canary"}}})
		case r.URL.Path == "/api/channels/chn_canary/messages" && r.Method == http.MethodPost:
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !strings.HasPrefix(body["nonce"], "fakeco-canary."+correlationID+".") || !strings.Contains(body["body"], correlationID) {
				t.Errorf("unexpected canary payload: %#v", body)
			}
			seq := int64(7)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": store.Message{ID: quoted, ChannelID: "chn_canary", ChannelSeq: &seq}})
		case r.URL.Path == "/api/channels/chn_canary/messages" && r.Method == http.MethodGet:
			cursor := r.URL.Query().Get("after_seq")
			cursors = append(cursors, cursor)
			if cursor == "7" {
				messages := make([]store.Message, 0, 100)
				for seq := int64(8); seq <= 107; seq++ {
					value := seq
					messages = append(messages, store.Message{ID: fmt.Sprintf("msg_noise_%d", seq), ChannelSeq: &value})
				}
				messages[0] = store.Message{
					ID: "msg_error", Body: "OpenClaw error for " + correlationID, Kind: store.MessageKindMessage,
					ChannelSeq: messages[0].ChannelSeq, QuotedMessageID: &quoted, Author: &store.User{ID: "usr_openclaw", Kind: "bot"},
				}
				_ = json.NewEncoder(w).Encode(store.MessagePage{Messages: messages, OldestSeq: 8, NewestSeq: 107, HasNewer: true})
				return
			}
			seq := int64(108)
			_ = json.NewEncoder(w).Encode(store.MessagePage{
				Messages: []store.Message{{
					ID: "msg_reply", Body: "fakeco-canary-ok " + correlationID, Kind: store.MessageKindMessage, ChannelSeq: &seq,
					QuotedMessageID: &quoted, Author: &store.User{ID: "usr_openclaw", Kind: "bot"},
				}},
				OldestSeq: 108, NewestSeq: 108,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	c := apiClient{
		opts: clientOptions{Server: server.URL, Token: "redacted", Workspace: "fakeco", Channel: "e2e-canary"},
		http: server.Client(),
	}
	result, err := c.runCanary(context.Background(), canaryOptions{
		CorrelationID:    correlationID,
		GatewayHealthURL: server.URL + "/gateway-healthz",
		Timeout:          time.Second,
		PollInterval:     time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "passed" || !result.GatewayPreflight || result.RequestMessageID != quoted || result.ResponseMessageID != "msg_reply" {
		t.Fatalf("unexpected canary result: %#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cursors) != 2 || cursors[0] != "7" || cursors[1] != "107" {
		t.Fatalf("unexpected canary cursors: %#v", cursors)
	}
	for i, got := range correlations {
		if got != correlationID {
			t.Fatalf("request %d correlation = %q, want %q", i, got, correlationID)
		}
	}
}

func TestRunCanaryRejectsBotSessionAndUnsafeInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/me" {
			_ = json.NewEncoder(w).Encode(map[string]any{"user": store.User{ID: "usr_bot", Kind: "bot"}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	c := apiClient{opts: clientOptions{Server: server.URL, Token: "redacted"}, http: server.Client()}

	_, err := c.runCanary(context.Background(), canaryOptions{CorrelationID: "fakeco bot", Timeout: time.Second, PollInterval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "correlation ID") {
		t.Fatalf("expected invalid correlation error, got %v", err)
	}
	_, err = c.runCanary(context.Background(), canaryOptions{CorrelationID: "fakeco-bot", Timeout: time.Second, PollInterval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "human session token") {
		t.Fatalf("expected bot session rejection, got %v", err)
	}
	_, err = c.runCanary(context.Background(), canaryOptions{CorrelationID: "fakeco-url", GatewayHealthURL: "https://user:pass@example.test/healthz", Timeout: time.Second, PollInterval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "without embedded credentials") {
		t.Fatalf("expected embedded credential rejection, got %v", err)
	}
	c.opts.Server = "https://user:pass@example.test"
	_, err = c.runCanary(context.Background(), canaryOptions{CorrelationID: "fakeco-server-url", Timeout: time.Second, PollInterval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "ClickClack server URL") {
		t.Fatalf("expected ClickClack embedded credential rejection, got %v", err)
	}
}

func TestProbeGatewayHealthRejectsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/healthz", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	c := apiClient{http: server.Client(), correlationID: "fakeco-redirect-test"}
	err := c.probeGatewayHealth(context.Background(), server.URL+"/redirect")
	if err == nil || !strings.Contains(err.Error(), http.StatusText(http.StatusFound)) {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestRunCanaryTimeoutCoversIdentityResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	c := apiClient{
		opts: clientOptions{Server: server.URL, Token: "redacted"},
		http: server.Client(),
	}
	started := time.Now()
	_, err := c.runCanary(context.Background(), canaryOptions{
		CorrelationID: "fakeco-timeout-test",
		Timeout:       20 * time.Millisecond,
		PollInterval:  time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected identity lookup deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("canary ignored overall timeout: %s", elapsed)
	}
}
