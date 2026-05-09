package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestMutationAndEphemeralEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads"), PushNotifier: notifier}).Handler())
	t.Cleanup(server.Close)

	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          second.ID,
		PushoverEnabled: true,
		PushoverUserKey: "u12345678901234567890123456789",
	}); err != nil {
		t.Fatal(err)
	}

	updatedChannel := patchJSON[struct {
		Channel store.Channel `json:"channel"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/channels/"+channels[0].ID, map[string]any{"name": "dock"})
	if updatedChannel.Channel.Name != "dock" || updatedChannel.Event.Type != "channel.updated" {
		t.Fatalf("unexpected channel update: %#v", updatedChannel)
	}
	message := postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", map[string]string{"body": "original"}).Message
	if len(notifier.notifications) != 1 || notifier.notifications[0].RecipientKey != "u12345678901234567890123456789" {
		t.Fatalf("expected one pushover notification, got %#v", notifier.notifications)
	}
	updatedMessage := patchJSON[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/messages/"+message.ID, map[string]string{"body": "edited"})
	if updatedMessage.Message.Body != "edited" || updatedMessage.Event.Type != "message.updated" {
		t.Fatalf("unexpected message update: %#v", updatedMessage)
	}
	deletedMessage := deleteJSONBody[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/messages/"+message.ID)
	if deletedMessage.Message.DeletedAt == nil || deletedMessage.Event.Type != "message.deleted" {
		t.Fatalf("unexpected message delete: %#v", deletedMessage)
	}
	ephemeral := postJSON[struct {
		Event store.Event `json:"event"`
	}](t, server.URL+"/api/realtime/ephemeral", map[string]any{"workspace_id": workspaces[0].ID, "channel_id": channels[0].ID, "type": "typing.started"})
	if ephemeral.Event.Type != "typing.started" || ephemeral.Event.Cursor != "" {
		t.Fatalf("unexpected ephemeral event: %#v", ephemeral.Event)
	}
	presence := postJSON[struct {
		Event store.Event `json:"event"`
	}](t, server.URL+"/api/realtime/ephemeral", map[string]any{"workspace_id": workspaces[0].ID, "type": "presence.changed", "payload": map[string]any{"status": "afk"}})
	if presence.Event.Type != "presence.changed" {
		t.Fatalf("unexpected presence event: %#v", presence.Event)
	}
	notifier.err = errors.New("pushover unavailable")
	postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/channels/"+channels[0].ID+"/messages", map[string]string{"body": "still succeeds"})
	notifier.err = nil
	updatedMe := patchJSON[struct {
		User store.User `json:"user"`
	}](t, server.URL+"/api/me", map[string]any{
		"display_name": "Owner",
		"notification_settings": map[string]any{
			"pushover_enabled":  true,
			"pushover_user_key": "u98765432109876543210987654321",
		},
	})
	if updatedMe.User.NotificationSettings == nil || !updatedMe.User.NotificationSettings.PushoverEnabled || updatedMe.User.NotificationSettings.PushoverUserKey == "" {
		t.Fatalf("expected profile notification settings, got %#v", updatedMe.User)
	}
	expectStatus(t, http.MethodPatch, server.URL+"/api/channels/"+channels[0].ID, bytes.NewReader([]byte(`{`)), http.StatusBadRequest)
	expectStatus(t, http.MethodPatch, server.URL+"/api/channels/missing", bytes.NewReader([]byte(`{"name":"missing"}`)), http.StatusBadRequest)
	expectStatus(t, http.MethodPatch, server.URL+"/api/messages/"+message.ID, bytes.NewReader([]byte(`{`)), http.StatusBadRequest)
	expectStatus(t, http.MethodPatch, server.URL+"/api/messages/"+message.ID, bytes.NewReader([]byte(`{"body":" "}`)), http.StatusBadRequest)
	expectStatus(t, http.MethodDelete, server.URL+"/api/messages/missing", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/realtime/ephemeral", bytes.NewReader([]byte(`{`)), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/realtime/ephemeral", bytes.NewReader([]byte(`{"workspace_id":"`+workspaces[0].ID+`","type":"bad"}`)), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/realtime/ephemeral", bytes.NewReader([]byte(`{"workspace_id":"missing","type":"typing.started"}`)), http.StatusForbidden)
}

func TestDirectTypingEphemeralIsLimitedToConversationMembers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "stranger@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	for _, userID := range []string{member.ID, stranger.ID} {
		if err := st.AddWorkspaceMember(ctx, workspace.ID, userID, "member"); err != nil {
			t.Fatal(err)
		}
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	server := httptest.NewServer(New(st, hub, Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	ownerConn := dialRealtimeAsUser(t, server.URL, workspace.ID, owner.ID)
	defer ownerConn.CloseNow()
	memberConn := dialRealtimeAsUser(t, server.URL, workspace.ID, member.ID)
	defer memberConn.CloseNow()
	strangerConn := dialRealtimeAsUser(t, server.URL, workspace.ID, stranger.ID)
	defer strangerConn.CloseNow()

	ephemeral := postJSONAsUser[struct {
		Event store.Event `json:"event"`
	}](t, owner.ID, server.URL+"/api/realtime/ephemeral", map[string]any{
		"workspace_id":           workspace.ID,
		"direct_conversation_id": dm.ID,
		"type":                   "typing.started",
	})
	if ephemeral.Event.Type != "typing.started" || ephemeral.Event.ChannelID != "" {
		t.Fatalf("unexpected dm typing event: %#v", ephemeral.Event)
	}
	if payload, ok := ephemeral.Event.Payload.(map[string]any); !ok || payload["direct_conversation_id"] != dm.ID || payload["user_id"] != owner.ID {
		t.Fatalf("unexpected dm typing payload: %#v", ephemeral.Event.Payload)
	}

	if event, ok := readEventTypeWithin(t, ownerConn, "typing.started", time.Second); !ok || event.ID != ephemeral.Event.ID {
		t.Fatalf("owner should receive own dm typing event, got %#v ok=%v", event, ok)
	}
	if event, ok := readEventTypeWithin(t, memberConn, "typing.started", time.Second); !ok || event.ID != ephemeral.Event.ID {
		t.Fatalf("dm member should receive dm typing event, got %#v ok=%v", event, ok)
	}
	if event, ok := readEventTypeWithin(t, strangerConn, "typing.started", 150*time.Millisecond); ok {
		t.Fatalf("workspace non-member received private dm typing event: %#v", event)
	}

	expectStatus(t, http.MethodPost, server.URL+"/api/realtime/ephemeral", bytes.NewReader([]byte(`{"workspace_id":"`+workspace.ID+`","direct_conversation_id":"`+dm.ID+`","channel_id":"chn_any","type":"typing.started"}`)), http.StatusBadRequest)
	reqBody := bytes.NewReader([]byte(`{"workspace_id":"` + workspace.ID + `","direct_conversation_id":"` + dm.ID + `","type":"typing.started"}`))
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/realtime/ephemeral", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-User", stranger.ID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected non-member publish forbidden, got %s", resp.Status)
	}
}

func dialRealtimeAsUser(t *testing.T, serverURL, workspaceID, userID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspaceID)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{userID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func readEventTypeWithin(t *testing.T, conn *websocket.Conn, eventType string, timeout time.Duration) (store.Event, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return store.Event{}, false
			}
			t.Fatal(err)
		}
		var event store.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == eventType {
			return event, true
		}
	}
}

type recordingNotifier struct {
	notifications []PushNotification
	err           error
}

func (r *recordingNotifier) Notify(_ context.Context, notification PushNotification) error {
	r.notifications = append(r.notifications, notification)
	return r.err
}

func patchJSON[T any](t *testing.T, endpoint string, body any) T {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPatch, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return doJSON[T](t, req)
}

func deleteJSONBody[T any](t *testing.T, endpoint string) T {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	return doJSON[T](t, req)
}

func doJSON[T any](t *testing.T, req *http.Request) T {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %s", req.Method, req.URL, resp.Status)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
