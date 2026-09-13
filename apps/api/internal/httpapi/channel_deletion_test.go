package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestChannelDeletionHTTP(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "channel-http-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "channel-http-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	_, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Channel Admin Bot",
		Scopes:      []string{"bot:admin"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	doomed := postJSONAsUser[struct {
		Channel store.Channel `json:"channel"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/channels", map[string]string{"name": "doomed"}).Channel
	upload := uploadFileAsUserWithContentType(t, owner.ID, server.URL+"/api/uploads", workspace.ID, "notes.txt", "text/plain", "doomed notes")
	created := postJSONAsUser[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, owner.ID, server.URL+"/api/channels/"+doomed.ID+"/messages", map[string]string{"body": "doomed", "upload_id": upload.ID})
	storedUpload, err := st.GetUpload(ctx, upload.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedUpload.StoragePath); err != nil {
		t.Fatalf("expected the upload object before deletion: %v", err)
	}

	previewURL := server.URL + "/api/channels/" + doomed.ID + "/deletion-preview"
	deleteURL := server.URL + "/api/channels/" + doomed.ID
	preview := getJSONAsUser[store.ChannelDeletionPreview](t, owner.ID, previewURL)
	if preview.Channel.ID != doomed.ID || preview.Counts.Messages != 1 || preview.Counts.Files != 1 || preview.Counts.FileBytes != int64(len("doomed notes")) || preview.Blocker != "" {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	expectStatusAsUser(t, member.ID, http.MethodGet, previewURL, nil, http.StatusForbidden)
	expectStatusAsUser(t, member.ID, http.MethodDelete, deleteURL, nil, http.StatusForbidden)
	expectStatusWithBearer(t, botToken.Token, http.MethodGet, previewURL, nil, http.StatusForbidden)
	expectStatusWithBearer(t, botToken.Token, http.MethodDelete, deleteURL, nil, http.StatusForbidden)
	expectStatusAsUser(t, owner.ID, http.MethodDelete, server.URL+"/api/channels/chn_missing", nil, http.StatusNotFound)

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID)
	memberConn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{member.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memberConn.Close(websocket.StatusNormalClosure, "done") })

	expectStatusAsUser(t, owner.ID, http.MethodDelete, deleteURL, nil, http.StatusNoContent)
	live := readEventType(t, memberConn, "channel.deleted")
	payload, ok := live.Payload.(map[string]any)
	if live.ChannelID != "" || !ok || payload["channel_id"] != doomed.ID || payload["deleted_by"] != owner.ID {
		t.Fatalf("unexpected live deletion event: %#v", live)
	}
	// A client that reconnects from one of the channel's events keeps its place
	// and replays forward to the deletion instead of being forced to resync.
	resumed, _, err := websocket.Dial(ctx, wsURL+"&after_cursor="+url.QueryEscape(created.Event.Cursor), &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{member.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.Close(websocket.StatusNormalClosure, "done") })
	if replayed := readEventType(t, resumed, "channel.deleted"); replayed.ID != live.ID {
		t.Fatalf("resumed socket replayed %#v, want %#v", replayed, live)
	}
	if _, err := os.Stat(storedUpload.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("expected the upload object to be removed, got %v", err)
	}
	expectStatusAsUser(t, owner.ID, http.MethodGet, previewURL, nil, http.StatusNotFound)
	expectStatusAsUser(t, owner.ID, http.MethodDelete, deleteURL, nil, http.StatusNotFound)
	auditLog := getJSONAsUser[struct {
		AuditLogEntries []store.AuditLogEntry `json:"audit_log_entries"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/audit-log")
	foundAudit := false
	for _, entry := range auditLog.AuditLogEntries {
		if entry.Action == "channel.deleted" && entry.TargetID == doomed.ID && entry.Metadata["name"] == "doomed" {
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatalf("expected a channel.deleted audit entry, got %#v", auditLog.AuditLogEntries)
	}

	solo := postJSONAsUser[struct {
		Workspace store.Workspace `json:"workspace"`
	}](t, owner.ID, server.URL+"/api/workspaces", map[string]string{"name": "Solo"}).Workspace
	only := postJSONAsUser[struct {
		Channel store.Channel `json:"channel"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+solo.ID+"/channels", map[string]string{"name": "only"}).Channel
	lastPreview := getJSONAsUser[store.ChannelDeletionPreview](t, owner.ID, server.URL+"/api/channels/"+only.ID+"/deletion-preview")
	if lastPreview.Blocker != store.ChannelDeletionBlockedLastChannel {
		t.Fatalf("expected last-channel blocker, got %#v", lastPreview)
	}
	expectStatusAsUser(t, owner.ID, http.MethodDelete, server.URL+"/api/channels/"+only.ID, nil, http.StatusConflict)
}
