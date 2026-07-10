package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestMessageEventCorrelationSurvivesResponseRealtimeAndRetrieval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "correlation-api@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "done") })

	messageResult, messageStatus, messageHeaders := postJSONWithCorrelation[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", "corr-api-message", map[string]string{
		"body": "message content stays out of event metadata",
	})
	if messageStatus != http.StatusCreated || messageHeaders.Get(correlationIDHeader) != "corr-api-message" {
		t.Fatalf("unexpected message response: status=%d correlation=%q", messageStatus, messageHeaders.Get(correlationIDHeader))
	}
	assertAPIEventPayloadValue(t, messageResult.Event, "correlation_id", "corr-api-message")
	assertAPIEventPayloadMissing(t, messageResult.Event, "body")
	liveMessage := readEventType(t, conn, "message.created")
	assertAPIEventPayloadValue(t, liveMessage, "correlation_id", "corr-api-message")

	retrievedMessages := getJSON[struct {
		Events []store.Event `json:"events"`
	}](t, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID))
	assertAPIEventPayloadValue(t, eventByID(t, retrievedMessages.Events, messageResult.Event.ID), "correlation_id", "corr-api-message")

	replyResult, replyStatus, replyHeaders := postJSONWithCorrelation[struct {
		Message     store.Message     `json:"message"`
		ThreadState store.ThreadState `json:"thread_state"`
		Events      []store.Event     `json:"events"`
	}](t, server.URL+"/api/messages/"+messageResult.Message.ID+"/thread/replies", "corr-api-reply", map[string]string{
		"body": "reply content stays out of event metadata",
	})
	if replyStatus != http.StatusCreated || replyHeaders.Get(correlationIDHeader) != "corr-api-reply" {
		t.Fatalf("unexpected reply response: status=%d correlation=%q", replyStatus, replyHeaders.Get(correlationIDHeader))
	}
	replyEvent := eventByType(t, replyResult.Events, "thread.reply_created")
	stateEvent := eventByType(t, replyResult.Events, "thread.state_updated")
	assertAPIEventPayloadValue(t, replyEvent, "correlation_id", "corr-api-reply")
	assertAPIEventPayloadMissing(t, replyEvent, "body")
	assertAPIEventPayloadMissing(t, stateEvent, "correlation_id")
	liveReply := readEventType(t, conn, "thread.reply_created")
	assertAPIEventPayloadValue(t, liveReply, "correlation_id", "corr-api-reply")

	retrievedReplies := getJSON[struct {
		Events []store.Event `json:"events"`
	}](t, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(messageResult.Event.Cursor))
	assertAPIEventPayloadValue(t, eventByID(t, retrievedReplies.Events, replyEvent.ID), "correlation_id", "corr-api-reply")

	invalidResult, invalidStatus, invalidHeaders := postJSONWithCorrelation[struct {
		Event store.Event `json:"event"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", "unsafe correlation", map[string]string{
		"body": "invalid caller correlation gets replaced",
	})
	generated := invalidHeaders.Get(correlationIDHeader)
	if invalidStatus != http.StatusCreated || generated == "unsafe correlation" || !validCorrelationID(generated) {
		t.Fatalf("invalid correlation was not safely replaced: status=%d correlation=%q", invalidStatus, generated)
	}
	assertAPIEventPayloadValue(t, invalidResult.Event, "correlation_id", generated)
}

func postJSONWithCorrelation[T any](t *testing.T, endpoint, correlationID string, body any) (T, int, http.Header) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(correlationIDHeader, correlationID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %s %s", endpoint, resp.Status, body)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out, resp.StatusCode, resp.Header.Clone()
}

func assertAPIEventPayloadValue(t *testing.T, event store.Event, key, want string) {
	t.Helper()
	payload, ok := event.Payload.(map[string]any)
	if !ok || payload[key] != want {
		t.Fatalf("event %s payload[%q] = %#v; want %q", event.ID, key, payload[key], want)
	}
}

func assertAPIEventPayloadMissing(t *testing.T, event store.Event, key string) {
	t.Helper()
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		t.Fatalf("event %s payload type = %T", event.ID, event.Payload)
	}
	if value, exists := payload[key]; exists {
		t.Fatalf("event %s unexpectedly has payload[%q] = %#v", event.ID, key, value)
	}
}

func eventByID(t *testing.T, events []store.Event, id string) store.Event {
	t.Helper()
	for _, event := range events {
		if event.ID == id {
			return event
		}
	}
	t.Fatalf("event %s not found in %#v", id, events)
	return store.Event{}
}

func eventByType(t *testing.T, events []store.Event, eventType string) store.Event {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("event type %s not found in %#v", eventType, events)
	return store.Event{}
}
