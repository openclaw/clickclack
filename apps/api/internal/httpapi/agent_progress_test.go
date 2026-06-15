package httpapi

import (
	"context"
	"encoding/json"
	"io"
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

// TestAgentProgressEphemeralAuthz is the acceptance gate for the agent.progress
// ephemeral frame:
//
//   - bot-token-only (a human session can never publish progress),
//   - gated by the existing bot write scopes so normal chat bots can publish
//     progress without an extra setup step,
//   - required to name exactly one concrete target so a private turn can never
//     broadcast to the whole workspace,
//   - DM targets bind to server-derived conversation members.
func TestAgentProgressEphemeralAuthz(t *testing.T) {
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
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	// A source bridge bot with normal chat write permissions.
	progressBot, progressToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Progress Bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, progressBot.ID, "bot"); err != nil {
		t.Fatal(err)
	}

	// A read-only bridge bot cannot publish progress because it cannot write
	// normal chat activity either.
	readBot, readToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Read Bot",
		Scopes:      []string{"bot:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, readBot.ID, "bot"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	ephemeralURL := server.URL + "/api/realtime/ephemeral"
	channelFrame := `{"workspace_id":"` + workspace.ID + `","channel_id":"` + channel.ID +
		`","type":"agent.progress","payload":{"turn_id":"t1","seq":1,"op":"append","line":{"id":"tool_1","kind":"tool","status":"start","toolName":"bash"}}}`

	// 1. Human session (no bot token) cannot publish agent.progress at all.
	expectStatus(t, http.MethodPost, ephemeralURL, strings.NewReader(channelFrame), http.StatusForbidden)

	// 2. A read-only bot is rejected.
	expectStatusWithBearer(t, readToken.Token, http.MethodPost, ephemeralURL, strings.NewReader(channelFrame), http.StatusForbidden)

	// 3. A normal bot:write bridge bot publishing to a channel target is accepted.
	expectStatusWithBearer(t, progressToken.Token, http.MethodPost, ephemeralURL, strings.NewReader(channelFrame), http.StatusAccepted)

	// 4. S1: a progress frame with NO concrete target is rejected. Without this
	//    it would fall through to the workspace-wide branch and leak detail to
	//    every member.
	noTarget := `{"workspace_id":"` + workspace.ID +
		`","type":"agent.progress","payload":{"turn_id":"t1","seq":2,"op":"append","line":{"id":"x","kind":"commentary"}}}`
	expectStatusWithBearer(t, progressToken.Token, http.MethodPost, ephemeralURL, strings.NewReader(noTarget), http.StatusBadRequest)

	// 5. Channel and DM targets are mutually exclusive.
	bothTargets := `{"workspace_id":"` + workspace.ID + `","channel_id":"` + channel.ID +
		`","direct_conversation_id":"dm_whatever","type":"agent.progress","payload":{"turn_id":"t1","seq":3,"op":"append","line":{"id":"x","kind":"commentary"}}}`
	expectStatusWithBearer(t, progressToken.Token, http.MethodPost, ephemeralURL, strings.NewReader(bothTargets), http.StatusBadRequest)

	// 6. DM progress binds to server-derived recipients (S1 private binding). The
	//    bot must be a DM member and hold dms:write (bundled in bot:write).
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{progressBot.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmFrame := `{"workspace_id":"` + workspace.ID + `","direct_conversation_id":"` + dm.ID +
		`","type":"agent.progress","payload":{"turn_id":"t2","seq":1,"op":"append","line":{"id":"r1","kind":"thinking","text":"private detail"}}}`
	dmResult, dmStatus := postJSONWithBearerStatus[struct {
		Event store.Event `json:"event"`
	}](t, progressToken.Token, ephemeralURL, dmFrame)
	if dmStatus != http.StatusAccepted {
		t.Fatalf("DM agent.progress: expected 202, got %d", dmStatus)
	}
	if dmResult.Event.Type != "agent.progress" {
		t.Fatalf("DM agent.progress: unexpected event type %q", dmResult.Event.Type)
	}
}

// TestAgentProgressDeliversOverRealtimeWithPrivateScoping is the end-to-end
// proof for the Agent Bridge Phase 1 producer path: a scoped source-bridge bot
// publishes agent.progress through the deployed gate, and the frame is
// delivered over the native realtime WS to authorized subscribers only.
//
//   - a channel-targeted frame reaches a workspace member subscribed on the WS
//     (producer -> gate -> hub -> client, the whole transport), and
//   - a DM-targeted frame reaches a DM member but NOT a workspace member who is
//     outside the conversation (Sentinel S1 private binding holds over the wire,
//     via server-derived recipients — the relay never names recipients).
func TestAgentProgressDeliversOverRealtimeWithPrivateScoping(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
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
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	// An ordinary workspace member who will subscribe to the realtime WS. This
	// is the "viewer" — it is NOT the bot, and (below) NOT a member of the DM.
	viewer, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Viewer", Email: "viewer@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, viewer.ID, "member"); err != nil {
		t.Fatal(err)
	}

	progressBot, progressToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Progress Bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, progressBot.ID, "bot"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	ephemeralURL := server.URL + "/api/realtime/ephemeral"
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID)

	// The viewer subscribes to the native realtime WS as a dev-auth user.
	viewerConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{viewer.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = viewerConn.Close(websocket.StatusNormalClosure, "done") })
	// websocket.Dial returns once the HTTP upgrade completes, but the server's
	// hub.Subscribe runs in the handler goroutine just after. Settle briefly so
	// the subscription is registered before the first publish, otherwise the
	// first frame can be published into the hub before this viewer is listening.
	time.Sleep(200 * time.Millisecond)

	// 1. Channel-targeted progress reaches the subscribed workspace member.
	channelFrame := `{"workspace_id":"` + workspace.ID + `","channel_id":"` + channel.ID +
		`","type":"agent.progress","payload":{"turn_id":"t1","seq":1,"op":"append","line":{"id":"tool_1","kind":"tool","status":"start","toolName":"bash"}}}`
	_, status := postJSONWithBearerStatus[struct{}](t, progressToken.Token, ephemeralURL, channelFrame)
	if status != http.StatusAccepted {
		t.Fatalf("channel agent.progress: expected 202, got %d", status)
	}
	channelEvent := readEventType(t, viewerConn, "agent.progress")
	if channelEvent.ChannelID != channel.ID {
		t.Fatalf("channel agent.progress delivered with wrong channel: %#v", channelEvent)
	}

	// 2. DM-targeted progress must NOT reach the viewer, who is not in the DM.
	//    The DM has only owner + the bot; the viewer is a workspace member but
	//    outside the conversation, so server-derived recipients exclude it.
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{progressBot.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmFrame := `{"workspace_id":"` + workspace.ID + `","direct_conversation_id":"` + dm.ID +
		`","type":"agent.progress","payload":{"turn_id":"t2","seq":1,"op":"append","line":{"id":"r1","kind":"thinking","text":"private detail"}}}`
	_, dmStatus := postJSONWithBearerStatus[struct{}](t, progressToken.Token, ephemeralURL, dmFrame)
	if dmStatus != http.StatusAccepted {
		t.Fatalf("dm agent.progress: expected 202, got %d", dmStatus)
	}

	// Post a follow-up channel frame as a sentinel. If the viewer receives the
	// DM frame at all (a leak), it arrives before this sentinel and the assert
	// below catches it. The sentinel itself proves the WS is still live and the
	// viewer is still receiving channel-scoped progress after the DM publish.
	sentinelFrame := `{"workspace_id":"` + workspace.ID + `","channel_id":"` + channel.ID +
		`","type":"agent.progress","payload":{"turn_id":"t1","seq":2,"op":"finalize","line":{"id":"tool_1","kind":"tool","status":"result"}}}`
	_, sentinelStatus := postJSONWithBearerStatus[struct{}](t, progressToken.Token, ephemeralURL, sentinelFrame)
	if sentinelStatus != http.StatusAccepted {
		t.Fatalf("sentinel agent.progress: expected 202, got %d", sentinelStatus)
	}

	nextEvent := readEventType(t, viewerConn, "agent.progress")
	payload, _ := nextEvent.Payload.(map[string]any)
	if payload == nil {
		t.Fatalf("expected agent.progress payload map, got %#v", nextEvent.Payload)
	}
	if text, _ := payload["text"].(string); text == "private detail" {
		t.Fatalf("DM-scoped progress leaked to a non-member over the WS: %#v", nextEvent)
	}
	if seq, _ := payload["seq"].(float64); seq != 2 {
		t.Fatalf("expected the sentinel channel frame (seq=2) next, got %#v", nextEvent)
	}
}

// postJSONWithBearerStatus posts a raw JSON body with a bearer token and returns
// the decoded body plus the HTTP status, for assertions on accepted responses.
func postJSONWithBearerStatus[T any](t *testing.T, token, endpoint, jsonBody string) (T, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(jsonBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out T
	if resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	} else {
		_, _ = io.ReadAll(resp.Body)
	}
	return out, resp.StatusCode
}
