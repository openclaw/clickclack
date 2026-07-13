package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	postgresstore "github.com/openclaw/clickclack/apps/api/internal/store/postgres"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
	"github.com/openclaw/clickclack/apps/api/internal/uploadstore"
)

func TestChatAPIVerticalSlice(t *testing.T) {
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
	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	hub := realtime.NewHub()
	server := httptest.NewServer(New(st, hub, Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	expectStatus(t, http.MethodHead, server.URL+"/", nil, http.StatusOK)

	me := getJSON[struct {
		User store.User `json:"user"`
	}](t, server.URL+"/api/me")
	if me.User.ID != owner.ID {
		t.Fatalf("expected owner %s, got %s", owner.ID, me.User.ID)
	}
	profile := patchJSON[struct {
		User store.User `json:"user"`
	}](t, server.URL+"/api/me", map[string]string{
		"display_name": "Peter Steinberger",
		"handle":       "@steipete",
		"avatar_url":   "https://example.com/avatar.png",
	})
	if profile.User.DisplayName != "Peter Steinberger" || profile.User.Handle != "steipete" || profile.User.AvatarURL == "" {
		t.Fatalf("unexpected profile response: %#v", profile.User)
	}

	workspaces := getJSON[struct {
		Workspaces []store.Workspace `json:"workspaces"`
	}](t, server.URL+"/api/workspaces")
	workspace := workspaces.Workspaces[0]
	createdWorkspace := postJSON[struct {
		Workspace store.Workspace `json:"workspace"`
	}](t, server.URL+"/api/workspaces", map[string]string{"name": "Side Dock"})
	if createdWorkspace.Workspace.Slug != "side-dock" {
		t.Fatalf("unexpected workspace slug %q", createdWorkspace.Workspace.Slug)
	}
	gotWorkspace := getJSON[struct {
		Workspace store.Workspace `json:"workspace"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID)
	if gotWorkspace.Workspace.ID != workspace.ID {
		t.Fatalf("unexpected workspace response: %#v", gotWorkspace.Workspace)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}

	channels := getJSON[struct {
		Channels []store.Channel `json:"channels"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID+"/channels")
	channel := channels.Channels[0]
	createdChannel := postJSON[struct {
		Channel store.Channel `json:"channel"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID+"/channels", map[string]string{"name": "random"})
	if createdChannel.Channel.Name != "random" {
		t.Fatalf("unexpected channel: %#v", createdChannel.Channel)
	}

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	created := postJSON[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{"body": "findable **lobster**"})
	if created.Message.ChannelSeq == nil || *created.Message.ChannelSeq != 1 {
		t.Fatalf("unexpected channel seq: %#v", created.Message.ChannelSeq)
	}
	if event := readEventType(t, conn, "message.created"); event.Type != "message.created" {
		t.Fatalf("unexpected websocket event %s", event.Type)
	} else if payload, ok := event.Payload.(map[string]any); !ok || payload["author_id"] != owner.ID || event.Seq == nil || *event.Seq != 1 {
		t.Fatalf("unexpected message.created event payload: %#v", event)
	}
	messageLookup := getJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/messages/"+created.Message.ID)
	if messageLookup.Message.ID != created.Message.ID || messageLookup.Message.ChannelID != channel.ID {
		t.Fatalf("unexpected message lookup payload: %#v", messageLookup.Message)
	}
	nonceCreated, nonceStatus := postJSONWithStatus[struct {
		Message store.Message `json:"message"`
		Event   *store.Event  `json:"event,omitempty"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{"body": "idempotent post", "nonce": "http-nonce-1"})
	if nonceStatus != http.StatusCreated || nonceCreated.Event == nil || nonceCreated.Message.Nonce != "http-nonce-1" {
		t.Fatalf("unexpected nonce create response: status=%d payload=%#v", nonceStatus, nonceCreated)
	}
	nonceReplay, replayStatus := postJSONWithStatus[struct {
		Message store.Message `json:"message"`
		Event   *store.Event  `json:"event,omitempty"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{"body": "idempotent post", "nonce": "http-nonce-1"})
	if replayStatus != http.StatusOK || nonceReplay.Message.ID != nonceCreated.Message.ID || nonceReplay.Event != nil {
		t.Fatalf("unexpected nonce replay response: status=%d payload=%#v", replayStatus, nonceReplay)
	}

	messages := getJSON[struct {
		Messages []store.Message `json:"messages"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages")
	if len(messages.Messages) != 2 {
		t.Fatalf("expected two root messages, got %d", len(messages.Messages))
	}

	reply, replyStatus := postJSONWithStatus[struct {
		Message     store.Message     `json:"message"`
		ThreadState store.ThreadState `json:"thread_state"`
		Events      []store.Event     `json:"events"`
	}](t, server.URL+"/api/messages/"+created.Message.ID+"/thread/replies", map[string]string{"body": "thread _reply_", "nonce": "http-thread-nonce-1"})
	if replyStatus != http.StatusCreated || reply.ThreadState.ReplyCount != 1 || len(reply.Events) != 2 || reply.Message.Nonce != "http-thread-nonce-1" {
		t.Fatalf("unexpected thread reply create: status=%d payload=%#v", replyStatus, reply)
	}
	replayedReply, replayedReplyStatus := postJSONWithStatus[struct {
		Message     store.Message     `json:"message"`
		ThreadState store.ThreadState `json:"thread_state"`
		Events      []store.Event     `json:"events"`
	}](t, server.URL+"/api/messages/"+created.Message.ID+"/thread/replies", map[string]string{"body": "thread _reply_", "nonce": "http-thread-nonce-1"})
	if replayedReplyStatus != http.StatusOK || replayedReply.Message.ID != reply.Message.ID || replayedReply.ThreadState.ReplyCount != 1 || len(replayedReply.Events) != 0 {
		t.Fatalf("unexpected thread reply replay: status=%d payload=%#v", replayedReplyStatus, replayedReply)
	}

	thread := getJSON[struct {
		Root        store.Message     `json:"root"`
		Replies     []store.Message   `json:"replies"`
		ThreadState store.ThreadState `json:"thread_state"`
	}](t, server.URL+"/api/messages/"+created.Message.ID+"/thread")
	if thread.Root.ID != created.Message.ID || len(thread.Replies) != 1 {
		t.Fatalf("unexpected thread payload: %#v", thread)
	}

	search := getJSON[struct {
		Results []store.SearchResult `json:"results"`
	}](t, server.URL+"/api/search?workspace_id="+url.QueryEscape(workspace.ID)+"&q=lobster")
	if len(search.Results) != 1 || search.Results[0].Message.ID != created.Message.ID {
		t.Fatalf("unexpected search results: %#v", search.Results)
	}

	upload := uploadFile(t, server.URL+"/api/uploads", workspace.ID, "note.txt", "hello upload")
	if upload.StoragePath != "" {
		t.Fatalf("upload response leaked storage path: %#v", upload)
	}
	attach := postJSON[map[string]bool](t, server.URL+"/api/messages/"+created.Message.ID+"/attachments", map[string]string{"upload_id": upload.ID})
	if !attach["ok"] {
		t.Fatal("expected attachment success")
	}
	body := getBody(t, server.URL+"/api/uploads/"+upload.ID)
	if body != "hello upload" {
		t.Fatalf("unexpected upload body %q", body)
	}
	resp, err := http.Get(server.URL + "/api/uploads/" + upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unexpected upload headers: %s %s", resp.Header.Get("X-Content-Type-Options"), resp.Header.Get("Content-Disposition"))
	}
	rawUpload := uploadFileWithoutPartContentType(t, server.URL+"/api/uploads", workspace.ID)
	if rawUpload.ContentType != "application/octet-stream" || rawUpload.Width != 33 || rawUpload.Height != 0 || rawUpload.DurationMS != 0 {
		t.Fatalf("unexpected raw upload metadata: %#v", rawUpload)
	}
	privateUpload := uploadFileAsUser(t, second.ID, server.URL+"/api/uploads", workspace.ID, "private.txt", "private upload")
	expectStatus(t, http.MethodPost, server.URL+"/api/messages/"+created.Message.ID+"/attachments", strings.NewReader(`{"upload_id":"`+privateUpload.ID+`"}`), http.StatusForbidden)
	expectStatusAsUser(t, second.ID, http.MethodPost, server.URL+"/api/messages/"+created.Message.ID+"/attachments", strings.NewReader(`{"upload_id":"`+privateUpload.ID+`"}`), http.StatusForbidden)

	reaction := postJSON[struct {
		Event store.Event `json:"event"`
	}](t, server.URL+"/api/messages/"+created.Message.ID+"/reactions", map[string]string{"emoji": "lobster"})
	if reaction.Event.Type != "reaction.added" {
		t.Fatalf("unexpected reaction event: %s", reaction.Event.Type)
	}
	duplicateReaction, duplicateStatus := postJSONWithStatus[struct {
		Event store.Event `json:"event"`
	}](t, server.URL+"/api/messages/"+created.Message.ID+"/reactions", map[string]string{"emoji": "lobster"})
	if duplicateStatus != http.StatusOK || duplicateReaction.Event.ID != "" {
		t.Fatalf("expected duplicate reaction no-op, status=%d event=%#v", duplicateStatus, duplicateReaction.Event)
	}
	deleteJSON(t, server.URL+"/api/messages/"+created.Message.ID+"/reactions/lobster")

	dm := postJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms", map[string]any{"workspace_id": workspace.ID, "member_ids": []string{second.ID}})
	if len(dm.Conversation.Members) != 2 {
		t.Fatalf("expected two dm members, got %d", len(dm.Conversation.Members))
	}
	reusedDM := postJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms", map[string]any{"workspace_id": workspace.ID, "member_ids": []string{second.ID}})
	if reusedDM.Conversation.ID != dm.Conversation.ID {
		t.Fatalf("expected repeated two-member DM create to reuse %s, got %s", dm.Conversation.ID, reusedDM.Conversation.ID)
	}
	dmMessage := postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/dms/"+dm.Conversation.ID+"/messages", map[string]string{"body": "private click"})
	if dmMessage.Message.DirectConversationID != dm.Conversation.ID {
		t.Fatalf("unexpected dm message: %#v", dmMessage.Message)
	}
	dms := getJSON[struct {
		Conversations []store.DirectConversation `json:"conversations"`
	}](t, server.URL+"/api/dms?workspace_id="+url.QueryEscape(workspace.ID))
	if len(dms.Conversations) != 1 {
		t.Fatalf("expected one dm conversation, got %d", len(dms.Conversations))
	}
	getDM := getJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms/"+dm.Conversation.ID)
	if getDM.Conversation.ID != dm.Conversation.ID {
		t.Fatalf("unexpected get dm response: %#v", getDM.Conversation)
	}
	expectStatus(t, http.MethodDelete, server.URL+"/api/dms/"+dm.Conversation.ID, nil, http.StatusOK)
	hiddenDMS := getJSON[struct {
		Conversations []store.DirectConversation `json:"conversations"`
	}](t, server.URL+"/api/dms?workspace_id="+url.QueryEscape(workspace.ID))
	if len(hiddenDMS.Conversations) != 0 {
		t.Fatalf("expected hidden dm to disappear from list, got %#v", hiddenDMS.Conversations)
	}
	hiddenGetDM := getJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms/"+dm.Conversation.ID)
	if hiddenGetDM.Conversation.ID != dm.Conversation.ID {
		t.Fatalf("hidden dm should remain directly accessible: %#v", hiddenGetDM.Conversation)
	}
	openedDM := postJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms/"+dm.Conversation.ID+"/open", nil)
	if openedDM.Conversation.ID != dm.Conversation.ID {
		t.Fatalf("expected open to restore hidden dm %s, got %s", dm.Conversation.ID, openedDM.Conversation.ID)
	}
	expectStatus(t, http.MethodDelete, server.URL+"/api/dms/"+dm.Conversation.ID, nil, http.StatusOK)
	reopenedDM := postJSON[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, server.URL+"/api/dms", map[string]any{"workspace_id": workspace.ID, "member_ids": []string{second.ID}})
	if reopenedDM.Conversation.ID != dm.Conversation.ID {
		t.Fatalf("expected reopen to reuse hidden dm %s, got %s", dm.Conversation.ID, reopenedDM.Conversation.ID)
	}
	dmMessages := getJSON[struct {
		Messages []store.Message `json:"messages"`
	}](t, server.URL+"/api/dms/"+dm.Conversation.ID+"/messages")
	if len(dmMessages.Messages) != 1 {
		t.Fatalf("expected one dm message, got %d", len(dmMessages.Messages))
	}

	webhook := postJSON[struct {
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/hooks/mattermost/"+channel.ID, map[string]string{"text": "from webhook"})
	if webhook.Message.Body != "from webhook" {
		t.Fatalf("unexpected webhook body %q", webhook.Message.Body)
	}
	slash := postForm[struct {
		Text    string        `json:"text"`
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/hooks/slash/"+channel.ID, url.Values{"command": {"/clack"}, "text": {"from slash"}})
	if slash.Text != "/clack from slash" || slash.Message.Body != slash.Text {
		t.Fatalf("unexpected slash response: %#v", slash)
	}

	events := getJSON[struct {
		Events []store.Event `json:"events"`
	}](t, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(created.Event.Cursor))
	if len(events.Events) == 0 {
		t.Fatal("expected recoverable events after cursor")
	}
}

func TestWorkspaceAdminHTTPUploadLifecycle(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Admin Bot",
		Scopes:      []string{"profile:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bot.Kind != "bot" {
		t.Fatalf("expected bot kind, got %#v", bot)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	expectStatus(t, http.MethodPatch, server.URL+"/api/workspaces/"+workspace.ID, strings.NewReader(`{}`), http.StatusBadRequest)
	expectStatusAsUser(t, member.ID, http.MethodPatch, server.URL+"/api/workspaces/"+workspace.ID, strings.NewReader(`{"name":"Member Edit"}`), http.StatusForbidden)
	expectStatusWithBearer(t, botToken.Token, http.MethodPatch, server.URL+"/api/workspaces/"+workspace.ID, strings.NewReader(`{"name":"Bot Edit"}`), http.StatusForbidden)

	iconUpload := uploadFileAsUserWithContentType(t, owner.ID, server.URL+"/api/uploads", workspace.ID, "icon.png", "image/png", "fake png")
	iconURL := "/api/uploads/" + iconUpload.ID
	update := patchJSON[struct {
		Workspace store.Workspace `json:"workspace"`
		Event     store.Event     `json:"event"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID, map[string]string{
		"name":     "Admin Harbor",
		"slug":     "admin-harbor",
		"icon_url": iconURL,
	})
	if update.Workspace.Name != "Admin Harbor" || update.Workspace.Slug != "admin-harbor" || update.Workspace.IconURL != iconURL || update.Event.Type != "workspace.updated" {
		t.Fatalf("unexpected workspace update: %#v", update)
	}

	if body := getBodyAsUser(t, member.ID, server.URL+"/api/uploads/"+iconUpload.ID); body != "fake png" {
		t.Fatalf("expected second member to read workspace icon, got %q", body)
	}

	storedUpload, err := st.GetUpload(ctx, iconUpload.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedUpload.StoragePath); err != nil {
		t.Fatalf("expected local upload object before delete: %v", err)
	}

	auditLog := getJSON[struct {
		AuditLogEntries []store.AuditLogEntry `json:"audit_log_entries"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID+"/audit-log")
	foundIconAudit := false
	for _, entry := range auditLog.AuditLogEntries {
		if entry.Action == "workspace.updated" && entry.Metadata["icon_url"] == iconURL {
			foundIconAudit = true
			break
		}
	}
	if !foundIconAudit {
		t.Fatalf("expected icon_url in workspace.updated audit metadata, got %#v", auditLog.AuditLogEntries)
	}

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/transfer-ownership", strings.NewReader(`{"user_id":"`+owner.ID+`"}`), http.StatusForbidden)
	transfer := postJSON[struct {
		Workspace store.Workspace `json:"workspace"`
		Event     store.Event     `json:"event"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID+"/transfer-ownership", map[string]string{"user_id": member.ID})
	if transfer.Event.Type != "workspace.ownership_transferred" {
		t.Fatalf("unexpected transfer event: %#v", transfer)
	}

	expectStatus(t, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID, nil, http.StatusForbidden)
	expectStatusAsUser(t, member.ID, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID, nil, http.StatusNoContent)
	if _, err := os.Stat(storedUpload.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("expected local upload object cleanup, got %v", err)
	}
}

func TestWorkspaceDeleteUploadCleanupRetriesAfterStorageFailure(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-cleanup@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	storage := &faultInjectedUploadStore{
		store: uploadstore.NewLocal(filepath.Join(dataDir, "uploads")),
		err:   errors.New("temporary object store outage"),
	}
	srv := New(st, realtime.NewHub(), Options{UploadStorage: storage})
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	upload := uploadFile(t, server.URL+"/api/uploads", workspace.ID, "retry.txt", "retry body")
	storedUpload, err := st.GetUpload(ctx, upload.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedUpload.StoragePath); err != nil {
		t.Fatalf("expected local upload object before delete: %v", err)
	}

	storage.failDeletes = true
	expectStatus(t, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID, nil, http.StatusNoContent)
	if _, err := st.GetWorkspace(ctx, workspace.ID, owner.ID); err == nil {
		t.Fatal("expected workspace metadata to be deleted despite storage cleanup failure")
	}
	if _, err := os.Stat(storedUpload.StoragePath); err != nil {
		t.Fatalf("expected failed storage cleanup to leave object for retry: %v", err)
	}
	pending, err := st.ListPendingUploadCleanups(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].StoragePath != storedUpload.StoragePath || pending[0].Attempts != 1 || !strings.Contains(pending[0].LastError, "temporary object store outage") {
		t.Fatalf("expected retained failed cleanup state, got %#v", pending)
	}

	storage.failDeletes = false
	if err := srv.CleanupPendingUploadObjects(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedUpload.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("expected retry cleanup to delete object, got %v", err)
	}
	pending, err = st.ListPendingUploadCleanups(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected retry cleanup state to be cleared, got %#v", pending)
	}
}

func TestPostgresWorkspaceAdminLifecycleAndCleanupRecovery(t *testing.T) {
	ctx := context.Background()
	st, scopedDSN := newIsolatedPostgresHTTPTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Postgres Owner", "postgres-owner-admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Postgres Member", Email: "postgres-member-admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	storage := &faultInjectedUploadStore{
		store: uploadstore.NewLocal(filepath.Join(dataDir, "uploads")),
		err:   errors.New("temporary object store outage"),
	}
	srv := New(st, realtime.NewHub(), Options{UploadStorage: storage})
	server := httptest.NewServer(srv.Handler())

	upload := uploadFileAsUserWithContentType(t, owner.ID, server.URL+"/api/uploads", workspace.ID, "postgres-cleanup.txt", "text/plain", "postgres cleanup body")
	storedUpload, err := st.GetUpload(ctx, upload.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated := patchJSON[struct {
		Workspace store.Workspace `json:"workspace"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID, map[string]string{"name": "Postgres Admin Harbor"})
	if updated.Workspace.Name != "Postgres Admin Harbor" {
		t.Fatalf("unexpected Postgres workspace update: %#v", updated.Workspace)
	}
	transfer := postJSON[struct {
		Workspace store.Workspace `json:"workspace"`
	}](t, server.URL+"/api/workspaces/"+workspace.ID+"/transfer-ownership", map[string]string{"user_id": member.ID})
	if transfer.Workspace.Role != store.WorkspaceRoleModerator {
		t.Fatalf("expected former owner response role to become moderator, got %#v", transfer.Workspace)
	}

	storage.failDeletes = true
	expectStatusAsUser(t, member.ID, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID, nil, http.StatusNoContent)
	if _, err := st.GetWorkspace(ctx, workspace.ID, member.ID); err == nil {
		t.Fatal("expected Postgres workspace metadata to be permanently deleted")
	}
	pending, err := st.ListPendingUploadCleanups(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].StoragePath != storedUpload.StoragePath || pending[0].Attempts != 1 {
		t.Fatalf("expected persisted Postgres cleanup failure, got %#v", pending)
	}
	server.Close()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := postgresstore.Open(scopedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	storage.failDeletes = false
	recovery := New(reopened, realtime.NewHub(), Options{UploadStorage: storage})
	if err := recovery.CleanupPendingUploadObjects(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedUpload.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("expected recovered Postgres cleanup to delete object, got %v", err)
	}
	pending, err = reopened.ListPendingUploadCleanups(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected recovered Postgres cleanup queue to drain, got %#v err=%v", pending, err)
	}
}

func newIsolatedPostgresHTTPTestStore(t *testing.T) (*postgresstore.Store, string) {
	t.Helper()
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Fatal(err)
	}
	schema := fmt.Sprintf("httpapi_workspace_admin_%d", time.Now().UnixNano())
	if _, err := adminDB.Exec(`CREATE SCHEMA ` + schema); err != nil {
		_ = adminDB.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	scopedDSN := parsed.String()
	st, err := postgresstore.Open(scopedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		_, _ = adminDB.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = adminDB.Close()
	})
	return st, scopedDSN
}

func TestCleanupPendingUploadObjectsDrainsBeyondDefaultBatch(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-cleanup-batch@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-cleanup-batch@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	localStorage := uploadstore.NewLocal(filepath.Join(dataDir, "uploads"))
	storage := &faultInjectedUploadStore{
		store: localStorage,
		err:   errors.New("temporary object store outage"),
	}
	srv := New(st, realtime.NewHub(), Options{UploadStorage: storage})
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)

	const uploadCount = uploadCleanupSweepLimit + 5
	storagePaths := make([]string, 0, uploadCount)
	for i := 0; i < uploadCount; i++ {
		ownerID := owner.ID
		if i%2 == 1 {
			ownerID = member.ID
		}
		saved, err := localStorage.Save(ctx, strings.NewReader("batch cleanup body"), uploadstore.SaveOptions{ContentType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		storagePaths = append(storagePaths, saved.Path)
		if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
			WorkspaceID: workspace.ID,
			OwnerID:     ownerID,
			Filename:    fmt.Sprintf("batch-%03d.txt", i),
			ContentType: "text/plain",
			ByteSize:    saved.ByteSize,
			StoragePath: saved.Path,
			Width:       0,
			Height:      0,
			DurationMS:  0,
		}); err != nil {
			t.Fatal(err)
		}
	}

	storage.failDeletes = true
	expectStatus(t, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID, nil, http.StatusNoContent)
	pending, err := st.ListPendingUploadCleanups(ctx, uploadCount+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != uploadCount {
		t.Fatalf("expected %d pending cleanup rows, got %d", uploadCount, len(pending))
	}

	storage.failDeletes = false
	storage.failPaths = make(map[string]bool, uploadCleanupSweepLimit)
	for _, storagePath := range storagePaths[:uploadCleanupSweepLimit] {
		storage.failPaths[storagePath] = true
	}
	if err := srv.CleanupPendingUploadObjects(ctx, 0); !errors.Is(err, storage.err) {
		t.Fatalf("expected poison page error, got %v", err)
	}
	pending, err = st.ListPendingUploadCleanups(ctx, uploadCount+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != uploadCleanupSweepLimit {
		t.Fatalf("expected only the poison page to remain after full sweep, got %d rows", len(pending))
	}
	storage.failPaths = nil
	if err := srv.CleanupPendingUploadObjects(ctx, 0); err != nil {
		t.Fatal(err)
	}
	pending, err = st.ListPendingUploadCleanups(ctx, uploadCount+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected all pending cleanup rows to drain, got %d", len(pending))
	}
	for _, storagePath := range storagePaths {
		if _, err := os.Stat(storagePath); !os.IsNotExist(err) {
			t.Fatalf("expected cleanup retry to delete %q, got %v", storagePath, err)
		}
	}
}

func TestCreateWorkspaceAllowedForUsersWithoutMemberships(t *testing.T) {
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
	if _, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	newUser, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "New User", Email: "new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "guest-create@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	expectStatusAsUser(t, newUser.ID, http.MethodPost, server.URL+"/api/workspaces", strings.NewReader(`{"name":"First Room"}`), http.StatusCreated)
	expectStatusAsUser(t, guest.ID, http.MethodPost, server.URL+"/api/workspaces", strings.NewReader(`{"name":"Guest Escape"}`), http.StatusForbidden)
}

func TestWorkspaceMembersEndpointAllowsMembersWithoutModerationAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "http-members-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "http-members-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "http-members-moderator@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "http-members-stranger@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	page := getJSONAsUser[store.WorkspaceMemberPage](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/members?limit=1")
	if len(page.Members) != 1 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("unexpected member page response: %#v", page)
	}
	if page.TotalCount == nil || *page.TotalCount != 3 {
		t.Fatalf("expected first-page total count 3, got %#v", page.TotalCount)
	}
	if page.TotalByRole == nil {
		t.Fatal("expected first-page role totals")
	}
	if got := *page.TotalByRole; got.Owner != 1 || got.Moderator != 1 || got.Member != 1 || got.Bot != 0 || got.Guest != 0 {
		t.Fatalf("unexpected role totals: %#v", got)
	}
	if page.Members[0].WorkspaceID != workspace.ID || page.Members[0].JoinedAt == "" {
		t.Fatalf("expected public membership metadata, got %#v", page.Members[0])
	}
	next := getJSONAsUser[store.WorkspaceMemberPage](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/members?limit=1&cursor="+url.QueryEscape(page.NextCursor))
	if next.TotalCount != nil {
		t.Fatalf("expected cursor page to omit total count, got %#v", next.TotalCount)
	}
	if next.TotalByRole != nil {
		t.Fatalf("expected cursor page to omit role totals, got %#v", next.TotalByRole)
	}
	filtered := getJSONAsUser[store.WorkspaceMemberPage](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/members?role=moderator")
	if filtered.TotalCount == nil || *filtered.TotalCount != 1 {
		t.Fatalf("expected filtered total count 1, got %#v", filtered.TotalCount)
	}
	if filtered.TotalByRole != nil {
		t.Fatalf("expected role-filter page to omit role totals, got %#v", filtered.TotalByRole)
	}
	expectStatusAsUser(t, member.ID, http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/moderation/members", nil, http.StatusForbidden)
	expectStatusAsUser(t, stranger.ID, http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/members", nil, http.StatusForbidden)
	expectStatusAsUser(t, member.ID, http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/members?limit=bad", nil, http.StatusBadRequest)
}

func TestJSONBodiesAreSizeLimited(t *testing.T) {
	t.Parallel()
	st := newHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	body := strings.NewReader(`{"token":"` + strings.Repeat("a", maxJSONBodyBytes+1) + `"}`)
	expectStatus(t, http.MethodPost, server.URL+"/api/auth/magic/consume", body, http.StatusRequestEntityTooLarge)
}

func TestHTTPDeadlinesSkipWebSocketUpgrades(t *testing.T) {
	t.Parallel()
	var normal deadlineRecorder
	withHTTPDeadlines(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(&normal, httptest.NewRequest(http.MethodPost, "/api/me", nil))
	if len(normal.readDeadlines) != 2 || normal.readDeadlines[0].IsZero() || !normal.readDeadlines[1].IsZero() {
		t.Fatalf("unexpected read deadlines: %#v", normal.readDeadlines)
	}
	if len(normal.writeDeadlines) != 0 {
		t.Fatalf("unexpected write deadlines: %#v", normal.writeDeadlines)
	}

	var websocket deadlineRecorder
	req := httptest.NewRequest(http.MethodGet, "/api/realtime/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	withHTTPDeadlines(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(&websocket, req)
	if len(websocket.readDeadlines) != 0 || len(websocket.writeDeadlines) != 0 {
		t.Fatalf("websocket request should not inherit HTTP deadlines: read=%#v write=%#v", websocket.readDeadlines, websocket.writeDeadlines)
	}
}

func TestMessagePageHTTPCursors(t *testing.T) {
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
	channel := channels[0]
	for i := 1; i <= 12; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("http page %02d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	base := server.URL + "/api/channels/" + channel.ID + "/messages"

	latest := getJSON[store.MessagePage](t, base+"?limit=5")
	expectHTTPSeqs(t, latest.Messages, 8, 12)
	if !latest.HasOlder || latest.HasNewer {
		t.Fatalf("unexpected latest metadata: %#v", latest)
	}

	after := getJSON[store.MessagePage](t, base+"?after_seq=3&limit=4")
	expectHTTPSeqs(t, after.Messages, 4, 7)
	if !after.HasOlder || !after.HasNewer {
		t.Fatalf("unexpected after metadata: %#v", after)
	}

	before := getJSON[store.MessagePage](t, base+"?before_seq=8&limit=3")
	expectHTTPSeqs(t, before.Messages, 5, 7)
	if !before.HasOlder || !before.HasNewer {
		t.Fatalf("unexpected before metadata: %#v", before)
	}

	around := getJSON[store.MessagePage](t, base+"?around_seq=6&limit=5")
	expectHTTPSeqs(t, around.Messages, 4, 8)
	if !around.HasOlder || !around.HasNewer {
		t.Fatalf("unexpected around metadata: %#v", around)
	}

	expectStatus(t, http.MethodGet, base+"?before_seq=4&after_seq=7", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodGet, base+"?before_seq=", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodGet, base+"?after_seq=bad", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodGet, base+"?around_seq=-1", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodGet, base+"?mode=history", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodGet, base+"?mode=latest&before_seq=4", nil, http.StatusBadRequest)
}

func TestRouteResolverAPI(t *testing.T) {
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
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "route-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceOnly, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Workspace Only", Email: "route-workspace-only@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, workspaceOnly.ID, "member"); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "thread root"})
	if err != nil {
		t.Fatal(err)
	}
	root, err = st.EnsureThreadRouteID(ctx, owner.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	channelRoute := getJSON[struct {
		Route store.RouteTarget `json:"route"`
	}](t, server.URL+"/api/routes/"+workspace.RouteID+"/"+channel.RouteID)
	if channelRoute.Route.TargetType != "channel" || channelRoute.Route.TargetID != channel.ID || channelRoute.Route.CanonicalPath != "/app/"+workspace.RouteID+"/"+channel.RouteID {
		t.Fatalf("unexpected channel route response: %#v", channelRoute.Route)
	}

	legacyChannelRoute := getJSON[struct {
		Route store.RouteTarget `json:"route"`
	}](t, server.URL+"/api/routes/"+workspace.ID+"/"+channel.ID)
	if legacyChannelRoute.Route != channelRoute.Route {
		t.Fatalf("legacy route did not canonicalize: %#v %#v", legacyChannelRoute.Route, channelRoute.Route)
	}
	scopeBot, scopeToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Route Scope Bot",
		Scopes:      []string{"profile:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if scopeBot.ID == "" {
		t.Fatal("expected route scope bot")
	}
	expectStatusWithBearer(t, scopeToken.Token, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/"+channel.RouteID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, scopeToken.Token, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/CMISSING", nil, http.StatusForbidden)

	dmRoute := getJSONAsUser[struct {
		Route store.RouteTarget `json:"route"`
	}](t, member.ID, server.URL+"/api/routes/"+workspace.RouteID+"/"+dm.RouteID)
	if dmRoute.Route.TargetType != "direct" || dmRoute.Route.TargetID != dm.ID {
		t.Fatalf("unexpected dm route response: %#v", dmRoute.Route)
	}
	legacyDMRoute := getJSONAsUser[struct {
		Route store.RouteTarget `json:"route"`
	}](t, member.ID, server.URL+"/api/routes/"+workspace.ID+"/"+dm.ID)
	if legacyDMRoute.Route != dmRoute.Route {
		t.Fatalf("legacy dm route did not canonicalize: %#v %#v", legacyDMRoute.Route, dmRoute.Route)
	}
	expectStatusAsUser(t, workspaceOnly.ID, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/"+dm.RouteID, nil, http.StatusNotFound)

	threadRoute := getJSON[struct {
		Route store.RouteTarget `json:"route"`
	}](t, server.URL+"/api/routes/"+workspace.RouteID+"/"+root.RouteID)
	if threadRoute.Route.TargetType != "thread" || threadRoute.Route.TargetID != root.ID || threadRoute.Route.ParentType != "channel" || threadRoute.Route.ParentID != channel.ID || threadRoute.Route.ParentRouteID != channel.RouteID {
		t.Fatalf("unexpected thread route response: %#v", threadRoute.Route)
	}
	dmRoot, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: member.ID, Body: "dm thread root"})
	if err != nil {
		t.Fatal(err)
	}
	dmRoot, err = st.EnsureThreadRouteID(ctx, member.ID, dmRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	dmThreadRoute := getJSONAsUser[struct {
		Route store.RouteTarget `json:"route"`
	}](t, member.ID, server.URL+"/api/routes/"+workspace.RouteID+"/"+dmRoot.RouteID)
	if dmThreadRoute.Route.TargetType != "thread" || dmThreadRoute.Route.ParentType != "direct" || dmThreadRoute.Route.ParentID != dm.ID || dmThreadRoute.Route.ParentRouteID != dm.RouteID {
		t.Fatalf("unexpected dm thread route response: %#v", dmThreadRoute.Route)
	}
	expectStatusAsUser(t, workspaceOnly.ID, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/"+dmRoot.RouteID, nil, http.StatusNotFound)
	expectStatus(t, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/Xbad", nil, http.StatusNotFound)

	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, http.MethodGet, server.URL+"/api/routes/"+otherWorkspace.RouteID+"/"+channel.RouteID, nil, http.StatusNotFound)
}

func TestReadEventsArePrivateAcrossWebSocketAndHTTPReplay(t *testing.T) {
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
	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	hub := realtime.NewHub()
	server := httptest.NewServer(New(st, hub, Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	created := postJSON[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{"body": "mark me read"})
	if created.Message.ChannelSeq == nil {
		t.Fatalf("expected channel seq: %#v", created.Message)
	}

	ownerRead := postJSONAsUser[struct {
		Receipt store.ReadReceipt `json:"receipt"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/read", map[string]int64{"seq": *created.Message.ChannelSeq})
	if ownerRead.Receipt.LastReadSeq != *created.Message.ChannelSeq {
		t.Fatalf("unexpected read receipt: %#v", ownerRead.Receipt)
	}
	ownerReadAgain := postJSONAsUser[struct {
		Receipt store.ReadReceipt `json:"receipt"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/read", map[string]int64{"seq": *created.Message.ChannelSeq})
	if ownerReadAgain.Receipt.LastReadSeq != *created.Message.ChannelSeq {
		t.Fatalf("unexpected idempotent read receipt: %#v", ownerReadAgain.Receipt)
	}
	expectStatus(t, http.MethodPost, server.URL+"/api/channels/"+channel.ID+"/read", strings.NewReader("{"), http.StatusBadRequest)

	ownerEvents := getJSONAsUser[struct {
		Events []store.Event `json:"events"`
	}](t, owner.ID, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(created.Event.Cursor))
	if len(ownerEvents.Events) != 1 || ownerEvents.Events[0].Type != "channel.read" {
		t.Fatalf("owner should receive own read event, got %#v", ownerEvents.Events)
	}

	secondEvents := getJSONAsUser[struct {
		Events []store.Event `json:"events"`
	}](t, second.ID, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(created.Event.Cursor))
	for _, event := range secondEvents.Events {
		if event.Type == "channel.read" {
			t.Fatalf("second user received private read event: %#v", secondEvents.Events)
		}
	}

	dm := postJSONAsUser[struct {
		Conversation store.DirectConversation `json:"conversation"`
	}](t, owner.ID, server.URL+"/api/dms", map[string]any{"workspace_id": workspace.ID, "member_ids": []string{second.ID}})
	dmMessage := postJSONAsUser[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, owner.ID, server.URL+"/api/dms/"+dm.Conversation.ID+"/messages", map[string]string{"body": "dm read"})
	if dmMessage.Message.ChannelSeq == nil {
		t.Fatalf("expected dm seq: %#v", dmMessage.Message)
	}
	ownerDMRead := postJSONAsUser[struct {
		Receipt store.ReadReceipt `json:"receipt"`
	}](t, owner.ID, server.URL+"/api/dms/"+dm.Conversation.ID+"/read", map[string]int64{"seq": *dmMessage.Message.ChannelSeq})
	if ownerDMRead.Receipt.LastReadSeq != *dmMessage.Message.ChannelSeq {
		t.Fatalf("unexpected dm read receipt: %#v", ownerDMRead.Receipt)
	}
	ownerDMReadAgain := postJSONAsUser[struct {
		Receipt store.ReadReceipt `json:"receipt"`
	}](t, owner.ID, server.URL+"/api/dms/"+dm.Conversation.ID+"/read", map[string]int64{"seq": *dmMessage.Message.ChannelSeq})
	if ownerDMReadAgain.Receipt.LastReadSeq != *dmMessage.Message.ChannelSeq {
		t.Fatalf("unexpected idempotent dm read receipt: %#v", ownerDMReadAgain.Receipt)
	}
	expectStatus(t, http.MethodPost, server.URL+"/api/dms/"+dm.Conversation.ID+"/read", strings.NewReader("{"), http.StatusBadRequest)
	secondDMEvents := getJSONAsUser[struct {
		Events []store.Event `json:"events"`
	}](t, second.ID, server.URL+"/api/realtime/events?workspace_id="+url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(dmMessage.Event.Cursor))
	for _, event := range secondDMEvents.Events {
		if event.Type == "dm.read" {
			t.Fatalf("second user received private dm read event: %#v", secondDMEvents.Events)
		}
	}
}

func TestHTTPErrorPathsAndSPA(t *testing.T) {
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
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	index := getBody(t, server.URL+"/")
	if !strings.Contains(index, "ClickClack") {
		t.Fatalf("expected embedded app shell, got %q", index)
	}
	fallback := getBody(t, server.URL+"/not-a-real-route")
	if !strings.Contains(fallback, "ClickClack") {
		t.Fatal("expected SPA fallback")
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected header auth success, got %s", resp.Status)
	}
	resp.Body.Close()

	link := postJSON[struct {
		Token string `json:"token"`
	}](t, server.URL+"/api/auth/magic/request", map[string]string{"email": "auth@example.com", "display_name": "Auth User"})
	auth := postJSON[struct {
		User    store.User    `json:"user"`
		Session store.Session `json:"session"`
	}](t, server.URL+"/api/auth/magic/consume", map[string]string{"token": link.Token})
	if auth.User.DisplayName != "Auth User" || auth.Session.Token == "" {
		t.Fatalf("unexpected auth payload: %#v", auth)
	}
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          auth.User.ID,
		PushoverEnabled: true,
		PushoverUserKey: "abcdefghijklmnopqrstuvwxyz1234",
	}); err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.Session.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected bearer auth success, got %s %s", resp.Status, string(body))
	}
	var sessionMe struct {
		User store.User `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionMe); err != nil {
		t.Fatal(err)
	}
	if sessionMe.User.NotificationSettings == nil || !sessionMe.User.NotificationSettings.PushoverEnabled || sessionMe.User.NotificationSettings.PushoverUserKey == "" {
		t.Fatalf("expected bearer /me notification settings, got %#v", sessionMe.User.NotificationSettings)
	}

	bot, botToken, err := st.CreateBot(context.Background(), store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "HTTP Bot",
		Handle:      "http-bot",
		Scopes:      []string{"messages:write", "realtime:read", "profile:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+botToken.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected bot bearer auth success, got %s %s", resp.Status, string(body))
	}
	resp.Body.Close()
	req, err = http.NewRequest(http.MethodPost, server.URL+"/api/channels/"+channel.ID+"/messages", strings.NewReader(`{"body":"bot hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+botToken.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected bot message success, got %s %s", resp.Status, string(body))
	}
	resp.Body.Close()
	page, err := st.ListMessages(context.Background(), channel.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	messages := page.Messages
	if messages[len(messages)-1].AuthorID != bot.ID || messages[len(messages)-1].Author == nil || messages[len(messages)-1].Author.Kind != "bot" {
		t.Fatalf("expected bot-authored message, got %#v", messages[len(messages)-1])
	}
	createdBot := postJSONAsUser[struct {
		Bot      store.User     `json:"bot"`
		BotToken store.BotToken `json:"bot_token"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots", map[string]any{
		"display_name": "API Bot",
		"handle":       "api-bot",
		"token_name":   "api",
		"scopes":       []string{"bot:read"},
	})
	if createdBot.Bot.Kind != "bot" || createdBot.BotToken.Token == "" {
		t.Fatalf("unexpected created bot payload: %#v", createdBot)
	}
	listedBots := getJSONAsUser[struct {
		Bots []store.BotWithTokens `json:"bots"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots")
	foundCreatedBot := false
	for _, item := range listedBots.Bots {
		if item.Bot.ID == createdBot.Bot.ID {
			foundCreatedBot = true
			if len(item.Tokens) != 1 || item.Tokens[0].Token != "" {
				t.Fatalf("expected one redacted token in list, got %#v", item.Tokens)
			}
		}
	}
	if !foundCreatedBot {
		t.Fatalf("created bot missing from list: %#v", listedBots.Bots)
	}
	rotatedToken := postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, owner.ID, server.URL+"/api/bots/"+createdBot.Bot.ID+"/tokens", map[string]any{
		"name":   "rotated",
		"scopes": []string{"messages:write"},
	})
	if rotatedToken.BotToken.Token == "" || rotatedToken.BotToken.BotUserID != createdBot.Bot.ID {
		t.Fatalf("unexpected rotated token payload: %#v", rotatedToken)
	}
	listedTokens := getJSONAsUser[struct {
		BotTokens []store.BotToken `json:"bot_tokens"`
	}](t, owner.ID, server.URL+"/api/bots/"+createdBot.Bot.ID+"/tokens")
	if len(listedTokens.BotTokens) != 2 {
		t.Fatalf("expected two bot tokens, got %#v", listedTokens.BotTokens)
	}
	revokedToken := postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, owner.ID, server.URL+"/api/bot-tokens/"+rotatedToken.BotToken.ID+"/revoke", map[string]any{})
	if revokedToken.BotToken.RevokedAt == nil {
		t.Fatalf("expected revoked_at on token, got %#v", revokedToken.BotToken)
	}
	req, err = http.NewRequest(http.MethodPost, server.URL+"/api/channels/"+channel.ID+"/messages", strings.NewReader(`{"body":"revoked should fail"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+rotatedToken.BotToken.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected revoked token to fail auth, got %s %s", resp.Status, string(body))
	}
	resp.Body.Close()
	createdInstall := postJSONAsUser[struct {
		AppInstallation store.AppInstallation `json:"app_installation"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations", map[string]any{
		"app_slug":     "openclaw",
		"display_name": "OpenClaw",
		"bot_user_id":  createdBot.Bot.ID,
		"config":       map[string]any{"default_channel_id": channel.ID},
	})
	if createdInstall.AppInstallation.AppSlug != "openclaw" || createdInstall.AppInstallation.BotUserID != createdBot.Bot.ID {
		t.Fatalf("unexpected app installation: %#v", createdInstall.AppInstallation)
	}
	listedInstalls := getJSONAsUser[struct {
		AppInstallations []store.AppInstallation `json:"app_installations"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations")
	if len(listedInstalls.AppInstallations) != 1 || listedInstalls.AppInstallations[0].ID != createdInstall.AppInstallation.ID {
		t.Fatalf("expected active app installation in list, got %#v", listedInstalls.AppInstallations)
	}
	createdTopic := postJSONAsUser[struct {
		Topic store.Topic `json:"topic"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/topics", map[string]any{
		"channel_id": channel.ID,
		"name":       "deploys",
	})
	if createdTopic.Topic.ChannelID != channel.ID || createdTopic.Topic.Name != "deploys" {
		t.Fatalf("unexpected created topic: %#v", createdTopic.Topic)
	}
	listedTopics := getJSONAsUser[struct {
		Topics []store.Topic `json:"topics"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/topics")
	if len(listedTopics.Topics) != 1 || listedTopics.Topics[0].ID != createdTopic.Topic.ID {
		t.Fatalf("expected topic in list, got %#v", listedTopics.Topics)
	}
	var signingSecret string
	var callbackPayload map[string]any
	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		timestamp := r.Header.Get("X-ClickClack-Timestamp")
		if timestamp == "" || r.Header.Get("X-ClickClack-Signature") != signSlashCallback(signingSecret, timestamp, body) {
			t.Fatalf("callback signature mismatch: timestamp=%q signature=%q body=%s", timestamp, r.Header.Get("X-ClickClack-Signature"), string(body))
		}
		if err := json.Unmarshal(body, &callbackPayload); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "in_channel",
			"text":          "deployed " + strings.TrimSpace(callbackPayload["text"].(string)),
		})
	}))
	defer callbackServer.Close()
	registeredCommand := postJSONAsUser[struct {
		SlashCommand store.SlashCommand `json:"slash_command"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands", map[string]any{
		"app_installation_id": createdInstall.AppInstallation.ID,
		"command":             "/deploy",
		"description":         "Deploy something",
		"callback_url":        callbackServer.URL,
		"bot_user_id":         createdBot.Bot.ID,
	})
	signingSecret = registeredCommand.SlashCommand.SigningSecret
	if signingSecret == "" || registeredCommand.SlashCommand.Command != "/deploy" {
		t.Fatalf("unexpected registered slash command: %#v", registeredCommand.SlashCommand)
	}
	listedCommands := getJSONAsUser[struct {
		SlashCommands []store.SlashCommand `json:"slash_commands"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands")
	if len(listedCommands.SlashCommands) != 1 || listedCommands.SlashCommands[0].SigningSecret != "" {
		t.Fatalf("expected one redacted slash command, got %#v", listedCommands.SlashCommands)
	}
	signedSlash := postForm[struct {
		Text    string        `json:"text"`
		Message store.Message `json:"message"`
	}](t, server.URL+"/api/hooks/slash/"+channel.ID, url.Values{"command": {"/deploy"}, "text": {"prod"}})
	if signedSlash.Text != "deployed prod" || signedSlash.Message.AuthorID != createdBot.Bot.ID {
		t.Fatalf("unexpected registered slash response: %#v", signedSlash)
	}
	if callbackPayload["trigger_id"] == "" || callbackPayload["channel_id"] != channel.ID {
		t.Fatalf("unexpected callback payload: %#v", callbackPayload)
	}
	revokedCommand := postJSONAsUser[struct {
		SlashCommand store.SlashCommand `json:"slash_command"`
	}](t, owner.ID, server.URL+"/api/slash-commands/"+registeredCommand.SlashCommand.ID+"/revoke", map[string]any{})
	if revokedCommand.SlashCommand.RevokedAt == nil {
		t.Fatalf("expected revoked_at on slash command, got %#v", revokedCommand.SlashCommand)
	}
	var eventSigningSecret string
	var eventPayload map[string]any
	eventCallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		timestamp := r.Header.Get("X-ClickClack-Timestamp")
		if r.Header.Get("X-ClickClack-Event-ID") == "" || timestamp == "" || r.Header.Get("X-ClickClack-Signature") != signSlashCallback(eventSigningSecret, timestamp, body) {
			t.Fatalf("event callback signature mismatch")
		}
		if err := json.Unmarshal(body, &eventPayload); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"ok": "true"})
	}))
	defer eventCallbackServer.Close()
	eventSubscription := postJSONAsUser[struct {
		EventSubscription store.EventSubscription `json:"event_subscription"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions", map[string]any{
		"app_installation_id": createdInstall.AppInstallation.ID,
		"event_types":         []string{"message.created"},
		"callback_url":        eventCallbackServer.URL,
	})
	eventSigningSecret = eventSubscription.EventSubscription.SigningSecret
	if eventSigningSecret == "" {
		t.Fatalf("expected one-time event signing secret: %#v", eventSubscription.EventSubscription)
	}
	listedSubscriptions := getJSONAsUser[struct {
		EventSubscriptions []store.EventSubscription `json:"event_subscriptions"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions")
	if len(listedSubscriptions.EventSubscriptions) != 1 || listedSubscriptions.EventSubscriptions[0].ID != eventSubscription.EventSubscription.ID {
		t.Fatalf("expected event subscription in list, got %#v", listedSubscriptions.EventSubscriptions)
	}
	topicMessage := postJSONAsUser[struct {
		Message store.Message `json:"message"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]any{"body": "deliver me", "topic_id": createdTopic.Topic.ID})
	if topicMessage.Message.TopicID != createdTopic.Topic.ID {
		t.Fatalf("expected topic_id on message, got %#v", topicMessage.Message)
	}
	if eventPayload == nil || eventPayload["event"].(map[string]any)["type"] != "message.created" {
		t.Fatalf("expected message.created event payload, got %#v", eventPayload)
	}
	deliveries := getJSONAsUser[struct {
		EventDeliveryAttempts []store.EventDeliveryAttempt `json:"event_delivery_attempts"`
	}](t, owner.ID, server.URL+"/api/event-subscriptions/"+eventSubscription.EventSubscription.ID+"/deliveries")
	if len(deliveries.EventDeliveryAttempts) == 0 || deliveries.EventDeliveryAttempts[0].ResponseStatus != http.StatusAccepted {
		t.Fatalf("expected accepted event delivery attempt, got %#v", deliveries.EventDeliveryAttempts)
	}
	revokedSubscription := postJSONAsUser[struct {
		EventSubscription store.EventSubscription `json:"event_subscription"`
	}](t, owner.ID, server.URL+"/api/event-subscriptions/"+eventSubscription.EventSubscription.ID+"/revoke", map[string]any{})
	if revokedSubscription.EventSubscription.RevokedAt == nil {
		t.Fatalf("expected revoked_at on event subscription, got %#v", revokedSubscription.EventSubscription)
	}
	connectedAccount := postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts", map[string]any{
		"user_id":             owner.ID,
		"provider":            "github",
		"provider_account_id": "octo-123",
		"display_name":        "octocat",
		"scopes":              []string{"repo:read"},
		"metadata":            map[string]any{"login": "octocat"},
	})
	if connectedAccount.ConnectedAccount.Provider != "github" || connectedAccount.ConnectedAccount.UserID != owner.ID {
		t.Fatalf("unexpected connected account: %#v", connectedAccount.ConnectedAccount)
	}
	accounts := getJSONAsUser[struct {
		ConnectedAccounts []store.ConnectedAccount `json:"connected_accounts"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts")
	if len(accounts.ConnectedAccounts) != 1 || accounts.ConnectedAccounts[0].ID != connectedAccount.ConnectedAccount.ID {
		t.Fatalf("expected connected account in list, got %#v", accounts.ConnectedAccounts)
	}
	revokedAccount := postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, owner.ID, server.URL+"/api/connected-accounts/"+connectedAccount.ConnectedAccount.ID+"/revoke", map[string]any{})
	if revokedAccount.ConnectedAccount.RevokedAt == nil {
		t.Fatalf("expected revoked_at on connected account, got %#v", revokedAccount.ConnectedAccount)
	}
	auditLog := getJSONAsUser[struct {
		AuditLogEntries []store.AuditLogEntry `json:"audit_log_entries"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/audit-log")
	if len(auditLog.AuditLogEntries) < 2 || auditLog.AuditLogEntries[0].Action != "connected_account.revoked" {
		t.Fatalf("expected connected account audit entries, got %#v", auditLog.AuditLogEntries)
	}
	revokedInstall := postJSONAsUser[struct {
		AppInstallation store.AppInstallation `json:"app_installation"`
	}](t, owner.ID, server.URL+"/api/app-installations/"+createdInstall.AppInstallation.ID+"/revoke", map[string]any{})
	if revokedInstall.AppInstallation.RevokedAt == nil {
		t.Fatalf("expected revoked_at on app installation, got %#v", revokedInstall.AppInstallation)
	}
	readOnlyBot, readOnlyToken, err := st.CreateBot(context.Background(), store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Read Bot",
		Scopes:      []string{"profile:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if readOnlyBot.Kind != "bot" {
		t.Fatalf("expected bot kind, got %#v", readOnlyBot)
	}
	req, err = http.NewRequest(http.MethodPost, server.URL+"/api/channels/"+channel.ID+"/messages", strings.NewReader(`{"body":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+readOnlyToken.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected bot scope failure, got %s %s", resp.Status, string(body))
	}
	resp.Body.Close()
	messageID := messages[len(messages)-1].ID
	for _, tc := range []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
	}{
		{"list workspaces", http.MethodGet, "/api/workspaces", "", ""},
		{"get workspace", http.MethodGet, "/api/workspaces/" + workspace.ID, "", ""},
		{"list channels", http.MethodGet, "/api/workspaces/" + workspace.ID + "/channels", "", ""},
		{"create channel", http.MethodPost, "/api/workspaces/" + workspace.ID + "/channels", `{"name":"bot-channel"}`, "application/json"},
		{"update channel", http.MethodPatch, "/api/channels/" + channel.ID, `{"name":"bot-channel"}`, "application/json"},
		{"list messages", http.MethodGet, "/api/channels/" + channel.ID + "/messages", "", ""},
		{"update message", http.MethodPatch, "/api/messages/" + messageID, `{"body":"blocked"}`, "application/json"},
		{"delete message", http.MethodDelete, "/api/messages/" + messageID, "", ""},
		{"mark channel read", http.MethodPost, "/api/channels/" + channel.ID + "/read", `{"seq":1}`, "application/json"},
		{"get thread", http.MethodGet, "/api/messages/" + messageID + "/thread", "", ""},
		{"create thread reply", http.MethodPost, "/api/messages/" + messageID + "/thread/replies", `{"body":"reply"}`, "application/json"},
		{"add reaction", http.MethodPost, "/api/messages/" + messageID + "/reactions", `{"emoji":"ok"}`, "application/json"},
		{"remove reaction", http.MethodDelete, "/api/messages/" + messageID + "/reactions/%F0%9F%91%8D", "", ""},
		{"list events", http.MethodGet, "/api/realtime/events?workspace_id=" + url.QueryEscape(workspace.ID), "", ""},
		{"websocket", http.MethodGet, "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID), "", ""},
		{"search", http.MethodGet, "/api/search?workspace_id=" + url.QueryEscape(workspace.ID) + "&q=bot", "", ""},
		{"get upload", http.MethodGet, "/api/uploads/missing", "", ""},
		{"attach upload", http.MethodPost, "/api/messages/" + messageID + "/attachments", `{"upload_id":"upl_missing"}`, "application/json"},
		{"list dms", http.MethodGet, "/api/dms?workspace_id=" + url.QueryEscape(workspace.ID), "", ""},
		{"create dm", http.MethodPost, "/api/dms", `{"workspace_id":"` + workspace.ID + `"}`, "application/json"},
		{"close dm", http.MethodDelete, "/api/dms/dm_missing", "", ""},
		{"open dm", http.MethodPost, "/api/dms/dm_missing/open", "", ""},
		{"list dm messages", http.MethodGet, "/api/dms/dm_missing/messages", "", ""},
		{"create dm message", http.MethodPost, "/api/dms/dm_missing/messages", `{"body":"dm"}`, "application/json"},
		{"mark dm read", http.MethodPost, "/api/dms/dm_missing/read", `{"seq":1}`, "application/json"},
		{"list bots", http.MethodGet, "/api/workspaces/" + workspace.ID + "/bots", "", ""},
		{"create bot", http.MethodPost, "/api/workspaces/" + workspace.ID + "/bots", `{"display_name":"Nope"}`, "application/json"},
		{"list bot tokens", http.MethodGet, "/api/bots/" + createdBot.Bot.ID + "/tokens", "", ""},
		{"create bot token", http.MethodPost, "/api/bots/" + createdBot.Bot.ID + "/tokens", `{"name":"Nope"}`, "application/json"},
		{"revoke bot token", http.MethodPost, "/api/bot-tokens/" + rotatedToken.BotToken.ID + "/revoke", `{}`, "application/json"},
		{"list app installations", http.MethodGet, "/api/workspaces/" + workspace.ID + "/app-installations", "", ""},
		{"create app installation", http.MethodPost, "/api/workspaces/" + workspace.ID + "/app-installations", `{"app_slug":"x"}`, "application/json"},
		{"revoke app installation", http.MethodPost, "/api/app-installations/" + createdInstall.AppInstallation.ID + "/revoke", `{}`, "application/json"},
		{"list slash commands", http.MethodGet, "/api/workspaces/" + workspace.ID + "/slash-commands", "", ""},
		{"create slash command", http.MethodPost, "/api/workspaces/" + workspace.ID + "/slash-commands", `{"command":"/x"}`, "application/json"},
		{"revoke slash command", http.MethodPost, "/api/slash-commands/" + registeredCommand.SlashCommand.ID + "/revoke", `{}`, "application/json"},
		{"list event subscriptions", http.MethodGet, "/api/workspaces/" + workspace.ID + "/event-subscriptions", "", ""},
		{"create event subscription", http.MethodPost, "/api/workspaces/" + workspace.ID + "/event-subscriptions", `{"event_types":["message.created"]}`, "application/json"},
		{"revoke event subscription", http.MethodPost, "/api/event-subscriptions/" + eventSubscription.EventSubscription.ID + "/revoke", `{}`, "application/json"},
		{"list event deliveries", http.MethodGet, "/api/event-subscriptions/" + eventSubscription.EventSubscription.ID + "/deliveries", "", ""},
		{"list audit log", http.MethodGet, "/api/workspaces/" + workspace.ID + "/audit-log", "", ""},
		{"list connected accounts", http.MethodGet, "/api/workspaces/" + workspace.ID + "/connected-accounts", "", ""},
		{"create connected account", http.MethodPost, "/api/workspaces/" + workspace.ID + "/connected-accounts", `{"provider":"x"}`, "application/json"},
		{"revoke connected account", http.MethodPost, "/api/connected-accounts/" + connectedAccount.ConnectedAccount.ID + "/revoke", `{}`, "application/json"},
		{"mattermost webhook", http.MethodPost, "/api/hooks/mattermost/" + channel.ID, `{"text":"hook"}`, "application/json"},
		{"slash command", http.MethodPost, "/api/hooks/slash/" + channel.ID, "command=/bot&text=hello", "application/x-www-form-urlencoded"},
		{"ephemeral", http.MethodPost, "/api/realtime/ephemeral", `{"workspace_id":"` + workspace.ID + `","type":"typing.started"}`, "application/json"},
	} {
		t.Run("read_only_bot_forbidden_"+tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+readOnlyToken.Token)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				payload, _ := io.ReadAll(resp.Body)
				t.Fatalf("%s %s: expected forbidden, got %s %s", tc.method, tc.path, resp.Status, string(payload))
			}
		})
	}
	postUploadForm := func(t *testing.T, endpoint, token, workspaceID string, want int) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("workspace_id", workspaceID); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint, &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload form: expected %d, got %s %s", want, resp.Status, string(payload))
		}
	}
	postUploadForm(t, server.URL+"/api/uploads", readOnlyToken.Token, workspace.ID, http.StatusForbidden)
	uploadBot, uploadToken, err := st.CreateBot(context.Background(), store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Upload Bot",
		Scopes:      []string{"uploads:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if uploadBot.Kind != "bot" {
		t.Fatalf("expected upload bot kind, got %#v", uploadBot)
	}
	postUploadForm(t, server.URL+"/api/uploads", uploadToken.Token, "ws_missing", http.StatusForbidden)
	expectStatusWithBearer(t, uploadToken.Token, http.MethodPost, server.URL+"/api/messages/"+messageID+"/attachments", strings.NewReader(`{"upload_id":"upl_missing"}`), http.StatusForbidden)
	noUploadServer := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(noUploadServer.Close)
	postUploadForm(t, noUploadServer.URL+"/api/uploads", uploadToken.Token, workspace.ID, http.StatusInternalServerError)

	expectStatus(t, http.MethodPatch, server.URL+"/api/me", strings.NewReader("{"), http.StatusBadRequest)
	expectStatus(t, http.MethodPatch, server.URL+"/api/me", strings.NewReader(`{"display_name":"Owner","handle":"x"}`), http.StatusBadRequest)
	expectStatus(t, http.MethodPatch, server.URL+"/api/me", strings.NewReader(`{"display_name":"Owner","avatar_url":"ftp://example.com/a.png"}`), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/workspaces", strings.NewReader("{"), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/auth/magic/request", strings.NewReader(`{"email":""}`), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/auth/magic/consume", strings.NewReader(`{"token":"missing"}`), http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/workspaces/missing/channels", strings.NewReader(`{"name":"x"}`), http.StatusBadRequest)
	expectStatus(t, http.MethodGet, server.URL+"/api/realtime/ws", nil, http.StatusBadRequest)
	expectStatus(t, http.MethodPost, server.URL+"/api/uploads", strings.NewReader("not multipart"), http.StatusBadRequest)
	{
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("workspace_id", "wsp_missing"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("file", "orphan.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("orphan")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/uploads", &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected invalid upload workspace to be forbidden, got %s", resp.Status)
		}
		entries, err := os.ReadDir(filepath.Join(dataDir, "uploads"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected invalid upload to leave no files, got %v", entries)
		}
	}
	expectStatus(t, http.MethodGet, server.URL+"/api/uploads/missing", nil, http.StatusNotFound)
	expectStatus(t, http.MethodGet, server.URL+"/api/search?workspace_id=missing&q=x", nil, http.StatusBadRequest)
}

func TestMagicLinkRequestRequiresLoopbackDevClient(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/request", strings.NewReader(`{"email":"remote@example.com"}`))
	req.RemoteAddr = "203.0.113.10:45678"
	req.Host = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	recorder := httptest.NewRecorder()
	New(nil, nil, Options{}).Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected remote dev magic-link request to be forbidden, got %d", recorder.Code)
	}
}

func TestMagicLinkRequestRejectsPublicReverseProxyHost(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/request", strings.NewReader(`{"email":"remote@example.com"}`))
	req.RemoteAddr = "127.0.0.1:45678"
	req.Host = "chat.example.com"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("X-Forwarded-Host", "chat.example.com")
	recorder := httptest.NewRecorder()
	New(nil, nil, Options{}).Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected proxied public dev magic-link request to be forbidden, got %d", recorder.Code)
	}
}

func TestMagicLinkRequestRejectsCrossSiteLocalBrowser(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/request", strings.NewReader(`{"email":"remote@example.com"}`))
	req.RemoteAddr = "127.0.0.1:45678"
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	New(nil, nil, Options{}).Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site local dev magic-link request to be forbidden, got %d", recorder.Code)
	}
}

func TestDevFallbackAuthRejectsCrossSiteLocalBrowser(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder := httptest.NewRecorder()
	New(nil, nil, Options{}).Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected cross-site local dev fallback auth to be unauthorized, got %d", recorder.Code)
	}
}

func TestMagicLinkConsumeRequiresJSONAndSameOrigin(t *testing.T) {
	t.Parallel()
	st := newHTTPStore(t)
	handler := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "https://chat.example.com"}}).Handler()

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{
			name:    "text plain",
			headers: map[string]string{"Content-Type": "text/plain"},
			want:    http.StatusUnsupportedMediaType,
		},
		{
			name:    "cross-site origin",
			headers: map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name:    "cross-site fetch metadata",
			headers: map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name:    "same origin reaches token validation",
			headers: map[string]string{"Content-Type": "application/json; charset=utf-8", "Origin": "https://chat.example.com", "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(`{"token":"missing"}`))
			req.Host = "chat.example.com"
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestMagicLinkConsumeAllowsTLSProxyOriginWithoutPublicURL(t *testing.T) {
	t.Parallel()
	st := newHTTPStore(t)
	handler := New(st, realtime.NewHub(), Options{}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(`{"token":"missing"}`))
	req.Host = "chat.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://chat.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected token validation to run, got %d: %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(`{"token":"missing"}`))
	req.Host = "chat.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://chat.example.com:8443")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected non-default origin port to be rejected, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestMagicLinkConsumeNormalizesPublicURLOrigin(t *testing.T) {
	t.Parallel()
	st := newHTTPStore(t)
	handler := New(st, realtime.NewHub(), Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "https://Chat.Example.com:443/app"}}).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(`{"token":"missing"}`))
	req.Host = "chat.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://chat.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected token validation to run, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestJSONBodiesAreBounded(t *testing.T) {
	t.Parallel()
	st := newHTTPStore(t)
	handler := New(st, realtime.NewHub(), Options{}).Handler()

	for _, tc := range []struct {
		name                 string
		body                 string
		unknownContentLength bool
	}{
		{
			name: "large value",
			body: `{"token":"` + strings.Repeat("x", maxJSONBodyBytes) + `"}`,
		},
		{
			name: "declared large tail",
			body: `{"token":"missing"}` + strings.Repeat(" ", maxJSONBodyBytes),
		},
		{
			name:                 "chunked large tail",
			body:                 `{"token":"missing"}` + strings.Repeat(" ", maxJSONBodyBytes),
			unknownContentLength: true,
		},
		{
			name:                 "chunked second json large tail",
			body:                 `{"token":"missing"}{}` + strings.Repeat(" ", maxJSONBodyBytes),
			unknownContentLength: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/magic/consume", strings.NewReader(tc.body))
			req.Host = "chat.example.com"
			req.Header.Set("Content-Type", "application/json")
			if tc.unknownContentLength {
				req.ContentLength = -1
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected body limit error, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHTTPServerTimeouts(t *testing.T) {
	t.Parallel()
	server := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if server.ReadHeaderTimeout != readHeaderTimeout {
		t.Fatalf("unexpected read header timeout %s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != idleTimeout {
		t.Fatalf("unexpected idle timeout %s", server.IdleTimeout)
	}
}

func getJSON[T any](t *testing.T, endpoint string) T {
	t.Helper()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %s %s", endpoint, resp.Status, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getJSONAsUser[T any](t *testing.T, userID, endpoint string) T {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %s %s", endpoint, resp.Status, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postJSON[T any](t *testing.T, endpoint string, body any) T {
	t.Helper()
	out, _ := postJSONWithStatus[T](t, endpoint, body)
	return out
}

func postJSONWithStatus[T any](t *testing.T, endpoint string, body any) (T, int) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %s %s", endpoint, resp.Status, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out, resp.StatusCode
}

func postJSONAsUser[T any](t *testing.T, userID, endpoint string, body any) T {
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
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %s %s", endpoint, resp.Status, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postForm[T any](t *testing.T, endpoint string, form url.Values) T {
	t.Helper()
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s: %s %s", endpoint, resp.Status, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func deleteJSON(t *testing.T, endpoint string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE %s: %s %s", endpoint, resp.Status, string(body))
	}
}

func expectStatus(t *testing.T, method, endpoint string, body io.Reader, status int) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: expected %d, got %s %s", method, endpoint, status, resp.Status, string(payload))
	}
}

func expectStatusAsUser(t *testing.T, userID, method, endpoint string, body io.Reader, status int) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s as %s: expected %d, got %s %s", method, endpoint, userID, status, resp.Status, string(payload))
	}
}

func expectStatusWithBearer(t *testing.T, token, method, endpoint string, body io.Reader, status int) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s with bearer: expected %d, got %s %s", method, endpoint, status, resp.Status, string(payload))
	}
}

type deadlineRecorder struct {
	httptest.ResponseRecorder
	readDeadlines  []time.Time
	writeDeadlines []time.Time
}

func (r *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.readDeadlines = append(r.readDeadlines, deadline)
	return nil
}

func (r *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.writeDeadlines = append(r.writeDeadlines, deadline)
	return nil
}

func uploadFile(t *testing.T, endpoint, workspaceID, filename, content string) store.Upload {
	t.Helper()
	return uploadFileAsUser(t, "", endpoint, workspaceID, filename, content)
}

func uploadFileAsUser(t *testing.T, userID, endpoint, workspaceID, filename, content string) store.Upload {
	t.Helper()
	return uploadFileAsUserWithContentType(t, userID, endpoint, workspaceID, filename, "", content)
}

func uploadFileAsUserWithContentType(t *testing.T, userID, endpoint, workspaceID, filename, contentType, content string) store.Upload {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("workspace_id", workspaceID); err != nil {
		t.Fatal(err)
	}
	var part io.Writer
	var err error
	if contentType == "" {
		part, err = writer.CreateFormFile("file", filename)
	} else {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		header.Set("Content-Type", contentType)
		part, err = writer.CreatePart(header)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if userID != "" {
		req.Header.Set("X-ClickClack-User", userID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %s %s", resp.Status, string(body))
	}
	var out struct {
		Upload store.Upload `json:"upload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Upload
}

func uploadFileWithoutPartContentType(t *testing.T, endpoint, workspaceID string) store.Upload {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"workspace_id": workspaceID,
		"width":        "33",
		"height":       "-1",
		"duration_ms":  "bad",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="raw.bin"`)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("raw")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(endpoint, writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("raw upload: %s %s", resp.Status, string(body))
	}
	var out struct {
		Upload store.Upload `json:"upload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Upload
}

func getBody(t *testing.T, endpoint string) string {
	t.Helper()
	resp, err := http.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 300 {
		t.Fatalf("GET %s: %s %s", endpoint, resp.Status, string(body))
	}
	return string(body)
}

func getBodyAsUser(t *testing.T, userID, endpoint string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 300 {
		t.Fatalf("GET %s as %s: %s %s", endpoint, userID, resp.Status, string(body))
	}
	return string(body)
}

func TestDisableDevAuthRequiresSession(t *testing.T) {
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
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{DisableDevAuth: true}).Handler())
	t.Cleanup(server.Close)

	expectStatus(t, http.MethodGet, server.URL+"/api/me", nil, http.StatusUnauthorized)
	expectStatus(t, http.MethodPatch, server.URL+"/api/me", strings.NewReader(`{"display_name":"Nope"}`), http.StatusUnauthorized)
	expectStatus(t, http.MethodPost, server.URL+"/api/auth/magic/request", strings.NewReader(`{"email":"no-token@example.com"}`), http.StatusNotImplemented)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/workspaces", ""},
		{http.MethodPost, "/api/workspaces", `{"name":"Private"}`},
		{http.MethodGet, "/api/workspaces/ws_missing", ""},
		{http.MethodGet, "/api/workspaces/ws_missing/channels", ""},
		{http.MethodPost, "/api/workspaces/ws_missing/channels", `{"name":"private"}`},
		{http.MethodPatch, "/api/channels/chn_missing", `{"name":"private"}`},
		{http.MethodGet, "/api/channels/chn_missing/messages", ""},
		{http.MethodPost, "/api/channels/chn_missing/messages", `{"body":"private"}`},
		{http.MethodPost, "/api/channels/chn_missing/read", `{"seq":1}`},
		{http.MethodGet, "/api/messages/msg_missing", ""},
		{http.MethodPatch, "/api/messages/msg_missing", `{"body":"private"}`},
		{http.MethodDelete, "/api/messages/msg_missing", ""},
		{http.MethodGet, "/api/messages/msg_missing/thread", ""},
		{http.MethodPost, "/api/messages/msg_missing/thread/replies", `{"body":"private"}`},
		{http.MethodPost, "/api/messages/msg_missing/reactions", `{"emoji":"ok"}`},
		{http.MethodDelete, "/api/messages/msg_missing/reactions/ok", ""},
		{http.MethodGet, "/api/realtime/events?workspace_id=ws_missing", ""},
		{http.MethodGet, "/api/realtime/ws?workspace_id=ws_missing", ""},
		{http.MethodPost, "/api/realtime/ephemeral", `{"workspace_id":"ws_missing","type":"typing.started"}`},
		{http.MethodGet, "/api/search?workspace_id=ws_missing&q=x", ""},
		{http.MethodPost, "/api/uploads", ""},
		{http.MethodPost, "/api/messages/msg_missing/attachments", `{"upload_id":"upl_missing"}`},
		{http.MethodGet, "/api/dms?workspace_id=ws_missing", ""},
		{http.MethodPost, "/api/dms", `{"workspace_id":"ws_missing"}`},
		{http.MethodGet, "/api/dms/dm_missing/messages", ""},
		{http.MethodPost, "/api/dms/dm_missing/messages", `{"body":"private"}`},
		{http.MethodPost, "/api/dms/dm_missing/read", `{"seq":1}`},
		{http.MethodPost, "/api/hooks/mattermost/chn_missing", `{"text":"private"}`},
		{http.MethodPost, "/api/hooks/slash/chn_missing", "command=/private"},
	} {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		expectStatus(t, tc.method, server.URL+tc.path, body, http.StatusUnauthorized)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected dev user header to be ignored, got %s", resp.Status)
	}

	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected session auth, got %s", resp.Status)
	}
}

func TestDevAuthFallbackRequiresLocalClient(t *testing.T) {
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
	handler := New(st, realtime.NewHub(), Options{}).Handler()
	for _, tc := range []struct {
		name       string
		remoteAddr string
		host       string
		userHeader bool
		forwarded  bool
	}{
		{name: "remote_first_user_fallback", remoteAddr: "203.0.113.10:45678", host: "127.0.0.1:8080"},
		{name: "remote_user_header", remoteAddr: "203.0.113.10:45678", host: "127.0.0.1:8080", userHeader: true},
		{name: "public_reverse_proxy", remoteAddr: "127.0.0.1:45678", host: "app.example.test", userHeader: true, forwarded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Host = tc.host
			if tc.userHeader {
				req.Header.Set("X-ClickClack-User", owner.ID)
			}
			if tc.forwarded {
				req.Header.Set("X-Forwarded-For", "203.0.113.10")
				req.Header.Set("X-Forwarded-Host", tc.host)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected remote dev auth request to be unauthorized, got %d", recorder.Code)
			}
		})
	}
}

func TestSecureCookiesFollowPublicURL(t *testing.T) {
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
	if _, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		GitHubOAuth: GitHubOAuthConfig{PublicURL: "https://app.clickclack.test"},
	}).Handler())
	t.Cleanup(server.Close)
	link := postJSON[struct {
		Token string `json:"token"`
	}](t, server.URL+"/api/auth/magic/request", map[string]string{"email": "secure-cookie@example.com"})
	resp, err := http.Post(server.URL+"/api/auth/magic/consume", "application/json", strings.NewReader(`{"token":"`+link.Token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cookie := findCookie(resp.Cookies(), "cc_session")
	if cookie == nil || !cookie.Secure {
		t.Fatalf("expected secure session cookie, got %#v", cookie)
	}
}

func TestNamespacedSessionCookiePolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Cookie User", Email: "cookie@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	cookieNames, err := authpolicy.NewCookieNames("prod", "https://chat.example.com")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, realtime.NewHub(), Options{
		CookieNames:    cookieNames,
		DisableDevAuth: true,
		GitHubOAuth:    GitHubOAuthConfig{PublicURL: "https://chat.example.com"},
	})

	req := httptest.NewRequest(http.MethodGet, "https://chat.example.com/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected legacy cookie to be ignored, got %d", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "https://chat.example.com/api/me", nil)
	req.AddCookie(&http.Cookie{Name: cookieNames.Session, Value: session.Token})
	recorder = httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected namespaced cookie authentication, got %d", recorder.Code)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req = httptest.NewRequest(http.MethodPost, "https://chat.example.com/test", nil)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: "foreign"})
	recorder = httptest.NewRecorder()
	srv.requireCookieCSRF(next).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected foreign cookie to bypass session CSRF handling, got %d", recorder.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "https://chat.example.com/test", nil)
	req.AddCookie(&http.Cookie{Name: cookieNames.Session, Value: session.Token})
	recorder = httptest.NewRecorder()
	srv.requireCookieCSRF(next).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected active session cookie to require CSRF proof, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	srv.setSessionCookie(recorder, httptest.NewRequest(http.MethodGet, "https://chat.example.com/", nil), session)
	cookie := findCookie(recorder.Result().Cookies(), cookieNames.Session)
	if cookie == nil || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected namespaced session cookie: %#v", cookie)
	}
}

func TestSessionCookiesDefaultSecureOutsideLocalDev(t *testing.T) {
	t.Parallel()
	session := store.Session{Token: "tok_test", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano)}
	for _, tc := range []struct {
		name       string
		options    Options
		url        string
		remoteAddr string
		wantSecure bool
	}{
		{
			name:       "production_http_fails_closed",
			options:    Options{DisableDevAuth: true},
			url:        "http://app.example.test/",
			remoteAddr: "203.0.113.10:45678",
			wantSecure: true,
		},
		{
			name:       "local_dev_http",
			options:    Options{},
			url:        "http://127.0.0.1:8080/",
			remoteAddr: "127.0.0.1:45678",
			wantSecure: false,
		},
		{
			name:       "local_dev_http_public_host",
			options:    Options{},
			url:        "http://app.example.test/",
			remoteAddr: "127.0.0.1:45678",
			wantSecure: true,
		},
		{
			name:       "local_public_url_does_not_downgrade_https_request",
			options:    Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "http://localhost:8080"}},
			url:        "https://localhost:8080/",
			remoteAddr: "127.0.0.1:45678",
			wantSecure: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			req.RemoteAddr = tc.remoteAddr
			recorder := httptest.NewRecorder()
			New(nil, nil, tc.options).setSessionCookie(recorder, req, session)
			cookie := findCookie(recorder.Result().Cookies(), "cc_session")
			if cookie == nil || cookie.Secure != tc.wantSecure {
				t.Fatalf("expected secure=%v session cookie, got %#v", tc.wantSecure, cookie)
			}
		})
	}
}

func TestRealtimeWebSocketOriginAndBearerProtocol(t *testing.T) {
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
	_, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspaces[0].ID,
		DisplayName: "Realtime Bot",
		Scopes:      []string{"realtime:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspaces[0].ID)
	if conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}, "X-ClickClack-User": []string{owner.ID}},
	}); err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "done")
		t.Fatal("expected cross-origin websocket dial to fail")
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{websocketBearerProtocolPrefix + token.Token},
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Subprotocol() != websocketBearerProtocolPrefix+token.Token {
		t.Fatalf("expected bearer subprotocol echo, got %q", conn.Subprotocol())
	}
	_ = conn.Close(websocket.StatusNormalClosure, "done")
}

func TestUploadResponseHeadersUseSafeContentTypes(t *testing.T) {
	t.Parallel()
	image := httptest.NewRecorder()
	setUploadResponseHeaders(image, store.Upload{Filename: "photo.png", ContentType: "Image/PNG; charset=utf-8"})
	if image.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(image.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("unexpected image headers: %#v", image.Header())
	}

	audio := httptest.NewRecorder()
	setUploadResponseHeaders(audio, store.Upload{Filename: "clip.m4a", ContentType: "audio/x-m4a"})
	if audio.Header().Get("Content-Type") != "audio/x-m4a" || !strings.HasPrefix(audio.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("unexpected audio headers: %#v", audio.Header())
	}

	html := httptest.NewRecorder()
	setUploadResponseHeaders(html, store.Upload{Filename: "index.html", ContentType: "text/html"})
	if html.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(html.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("unexpected html headers: %#v", html.Header())
	}
	if html.Header().Get("X-Content-Type-Options") != "nosniff" || html.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("missing hardening headers: %#v", html.Header())
	}
}

func TestUploadBodyErrorsClassifyOversizedRequests(t *testing.T) {
	t.Parallel()
	tooLarge := httptest.NewRecorder()
	writeUploadBodyError(tooLarge, &http.MaxBytesError{Limit: maxUploadBytes}, http.StatusInternalServerError)
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized upload to be 413, got %d", tooLarge.Code)
	}

	readFailure := httptest.NewRecorder()
	writeUploadBodyError(readFailure, errors.New("copy failed"), http.StatusInternalServerError)
	if readFailure.Code != http.StatusInternalServerError {
		t.Fatalf("expected fallback status, got %d", readFailure.Code)
	}

	quota := httptest.NewRecorder()
	writeUploadBodyError(quota, store.ErrUploadQuotaExceeded, http.StatusInternalServerError)
	if quota.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected quota error to be 413, got %d", quota.Code)
	}
}

func TestUploadQuotaReaderStopsOverBudget(t *testing.T) {
	t.Parallel()
	reader := &uploadQuotaReader{reader: strings.NewReader("abc"), remaining: 2}
	body, err := io.ReadAll(reader)
	if !errors.Is(err, store.ErrUploadQuotaExceeded) {
		t.Fatalf("expected quota error, got body=%q err=%v", body, err)
	}
}

func TestUploadReservesQuotaBeforeObjectStorage(t *testing.T) {
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
	storage := &quotaObservingUploadStore{
		store:       st,
		workspaceID: workspaces[0].ID,
		userID:      owner.ID,
		observed:    make(chan store.UploadQuota, 1),
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadStorage: storage}).Handler())
	t.Cleanup(server.Close)

	upload := uploadFile(t, server.URL+"/api/uploads", workspaces[0].ID, "reserved.txt", "reserved body")
	observed := <-storage.observed
	if observed.UsedCount != 1 || observed.UsedBytes != int64(maxUploadBytes) {
		t.Fatalf("storage started without full in-flight reservation: %#v", observed)
	}
	quota, err := st.UploadQuota(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quota.UsedCount != 1 || quota.UsedBytes != upload.ByteSize {
		t.Fatalf("reservation was not replaced by final upload row: quota=%#v upload=%#v", quota, upload)
	}
}

func TestBotGenericRoutesRequireDMScopeForDirectMessages(t *testing.T) {
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
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "No DM Scope Bot",
		Scopes:      []string{"messages:read", "messages:write", "threads:read", "threads:write", "uploads:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{bot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: bot.ID, Body: "bot dm"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     bot.ID,
		Filename:    "dm.txt",
		ContentType: "text/plain",
		ByteSize:    7,
		StoragePath: filepath.Join(dataDir, "uploads", "dm.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: message.ID, UploadID: upload.ID, UserID: bot.ID}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/messages/" + message.ID, ""},
		{http.MethodPatch, "/api/messages/" + message.ID, `{"body":"blocked"}`},
		{http.MethodDelete, "/api/messages/" + message.ID, ""},
		{http.MethodGet, "/api/messages/" + message.ID + "/thread", ""},
		{http.MethodPost, "/api/messages/" + message.ID + "/thread/replies", `{"body":"blocked"}`},
		{http.MethodPost, "/api/messages/" + message.ID + "/reactions", `{"emoji":"ok"}`},
		{http.MethodDelete, "/api/messages/" + message.ID + "/reactions/" + url.PathEscape("ok"), ""},
		{http.MethodPost, "/api/messages/" + message.ID + "/attachments", `{"upload_id":"` + upload.ID + `"}`},
		{http.MethodGet, "/api/uploads/" + upload.ID, ""},
		{http.MethodPost, "/api/realtime/ephemeral", `{"workspace_id":"` + workspace.ID + `","direct_conversation_id":"` + dm.ID + `","type":"typing.started"}`},
	}
	for _, tc := range cases {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		expectStatusWithBearer(t, token.Token, tc.method, server.URL+tc.path, body, http.StatusForbidden)
	}

	readBot, readToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "DM Read Bot",
		Scopes:      []string{"messages:read", "dms:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	readDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{readBot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	readMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: readDM.ID, AuthorID: owner.ID, Body: "visible dm"})
	if err != nil {
		t.Fatal(err)
	}
	expectStatusWithBearer(t, readToken.Token, http.MethodGet, server.URL+"/api/messages/"+readMessage.ID, nil, http.StatusOK)

	writeBot, writeToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "DM Write Bot",
		Scopes:      []string{"uploads:write", "messages:write", "dms:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{writeBot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: writeDM.ID, AuthorID: writeBot.ID, Body: "attach here"})
	if err != nil {
		t.Fatal(err)
	}
	writeUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     writeBot.ID,
		Filename:    "retry.txt",
		ContentType: "text/plain",
		ByteSize:    5,
		StoragePath: filepath.Join(dataDir, "uploads", "retry.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	attachBody := `{"upload_id":"` + writeUpload.ID + `"}`
	expectStatusWithBearer(t, writeToken.Token, http.MethodPost, server.URL+"/api/messages/"+writeMessage.ID+"/attachments", strings.NewReader(attachBody), http.StatusOK)
	expectStatusWithBearer(t, writeToken.Token, http.MethodPost, server.URL+"/api/messages/"+writeMessage.ID+"/attachments", strings.NewReader(attachBody), http.StatusOK)

	otherWriteDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{writeBot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	otherWriteMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: otherWriteDM.ID, AuthorID: writeBot.ID, Body: "other dm"})
	if err != nil {
		t.Fatal(err)
	}
	otherDMUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     writeBot.ID,
		Filename:    "other-dm.txt",
		ContentType: "text/plain",
		ByteSize:    8,
		StoragePath: filepath.Join(dataDir, "uploads", "other-dm.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: otherWriteMessage.ID, UploadID: otherDMUpload.ID, UserID: writeBot.ID}); err != nil {
		t.Fatal(err)
	}
	reuseBody := `{"upload_id":"` + otherDMUpload.ID + `"}`
	expectStatusWithBearer(t, writeToken.Token, http.MethodPost, server.URL+"/api/messages/"+writeMessage.ID+"/attachments", strings.NewReader(reuseBody), http.StatusForbidden)
}

type quotaObservingUploadStore struct {
	store       store.Store
	workspaceID string
	userID      string
	observed    chan store.UploadQuota
}

func (s *quotaObservingUploadStore) Save(ctx context.Context, body io.Reader, _ uploadstore.SaveOptions) (uploadstore.SavedObject, error) {
	quota, err := s.store.UploadQuota(ctx, s.workspaceID, s.userID)
	if err != nil {
		return uploadstore.SavedObject{}, err
	}
	s.observed <- quota
	size, err := io.Copy(io.Discard, body)
	if err != nil {
		return uploadstore.SavedObject{}, err
	}
	return uploadstore.SavedObject{Path: "memory://reserved-upload", ByteSize: size}, nil
}

func (s *quotaObservingUploadStore) Delete(context.Context, string) error {
	return nil
}

func (s *quotaObservingUploadStore) ServeHTTP(http.ResponseWriter, *http.Request, uploadstore.Object) error {
	return uploadstore.ErrNotFound
}

type faultInjectedUploadStore struct {
	store       uploadstore.Store
	failDeletes bool
	failPaths   map[string]bool
	err         error
}

func (s *faultInjectedUploadStore) Save(ctx context.Context, body io.Reader, options uploadstore.SaveOptions) (uploadstore.SavedObject, error) {
	return s.store.Save(ctx, body, options)
}

func (s *faultInjectedUploadStore) Delete(ctx context.Context, path string) error {
	if s.failDeletes || s.failPaths[path] {
		return s.err
	}
	return s.store.Delete(ctx, path)
}

func (s *faultInjectedUploadStore) ServeHTTP(w http.ResponseWriter, r *http.Request, object uploadstore.Object) error {
	return s.store.ServeHTTP(w, r, object)
}

func TestUploadRejectsInvalidMultipartShapes(t *testing.T) {
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
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	postUploadPath := func(t *testing.T, path string, body *bytes.Buffer, contentType string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	postUpload := func(t *testing.T, body *bytes.Buffer, contentType string) int {
		t.Helper()
		return postUploadPath(t, "/api/uploads", body, contentType)
	}

	var fileFirst bytes.Buffer
	fileFirstWriter := multipart.NewWriter(&fileFirst)
	part, err := fileFirstWriter.CreateFormFile("file", "early.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("early")); err != nil {
		t.Fatal(err)
	}
	if err := fileFirstWriter.WriteField("workspace_id", workspaces[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := fileFirstWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if got := postUpload(t, &fileFirst, fileFirstWriter.FormDataContentType()); got != http.StatusBadRequest {
		t.Fatalf("file before workspace_id: got %d", got)
	}

	var queryWorkspace bytes.Buffer
	queryWorkspaceWriter := multipart.NewWriter(&queryWorkspace)
	part, err = queryWorkspaceWriter.CreateFormFile("file", "early.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("early")); err != nil {
		t.Fatal(err)
	}
	if err := queryWorkspaceWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if got := postUploadPath(t, "/api/uploads?workspace_id="+url.QueryEscape(workspaces[0].ID), &queryWorkspace, queryWorkspaceWriter.FormDataContentType()); got != http.StatusCreated {
		t.Fatalf("file before form workspace_id with query workspace_id: got %d", got)
	}

	var mismatch bytes.Buffer
	mismatchWriter := multipart.NewWriter(&mismatch)
	if err := mismatchWriter.WriteField("workspace_id", "wsp_other"); err != nil {
		t.Fatal(err)
	}
	part, err = mismatchWriter.CreateFormFile("file", "mismatch.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("mismatch")); err != nil {
		t.Fatal(err)
	}
	if err := mismatchWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if got := postUploadPath(t, "/api/uploads?workspace_id="+url.QueryEscape(workspaces[0].ID), &mismatch, mismatchWriter.FormDataContentType()); got != http.StatusBadRequest {
		t.Fatalf("mismatched form/query workspace_id: got %d", got)
	}

	var duplicate bytes.Buffer
	duplicateWriter := multipart.NewWriter(&duplicate)
	if err := duplicateWriter.WriteField("workspace_id", workspaces[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		part, err := duplicateWriter.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := duplicateWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if got := postUpload(t, &duplicate, duplicateWriter.FormDataContentType()); got != http.StatusBadRequest {
		t.Fatalf("duplicate file: got %d", got)
	}

	invalid := bytes.NewBufferString("not a multipart body")
	if got := postUpload(t, invalid, "multipart/form-data; boundary=missing"); got != http.StatusBadRequest {
		t.Fatalf("invalid multipart: got %d", got)
	}
}

func TestQueryHelpersParseValues(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/?limit=42&after_seq=123", nil)
	if got := queryInt(req, "limit", 10); got != 42 {
		t.Fatalf("unexpected int query value %d", got)
	}
	if got := queryInt64(req, "after_seq", 10); got != 123 {
		t.Fatalf("unexpected int64 query value %d", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/?after_seq=bad", nil)
	if got := queryInt64(req, "after_seq", 10); got != 10 {
		t.Fatalf("unexpected fallback int64 query value %d", got)
	}
	if got := websocketBearerToken(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("unexpected bearer token %q", got)
	}
	server := New(nil, nil, Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "https://app.clickclack.test/path"}})
	patterns := server.websocketOriginPatterns(httptest.NewRequest(http.MethodGet, "/", nil))
	if len(patterns) != 1 || patterns[0] != "https://app.clickclack.test" {
		t.Fatalf("unexpected websocket origin patterns: %#v", patterns)
	}
	if patterns := New(nil, nil, Options{GitHubOAuth: GitHubOAuthConfig{PublicURL: "%"}}).websocketOriginPatterns(httptest.NewRequest(http.MethodGet, "/", nil)); patterns != nil {
		t.Fatalf("unexpected invalid websocket origin patterns: %#v", patterns)
	}
}

func TestDirectRealtimeEventsRespectGuestDemotion(t *testing.T) {
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
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "live-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "live-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: moderator.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	_, event, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: moderator.ID, Body: "hidden live event"})
	if err != nil {
		t.Fatal(err)
	}
	server := New(st, realtime.NewHub(), Options{})
	if server.shouldDeliverEventToActor(ctx, event, member.ID) {
		t.Fatalf("direct realtime event delivered to demoted guest: %#v", event)
	}
	if !server.shouldDeliverEventToActor(ctx, event, moderator.ID) {
		t.Fatalf("direct realtime event denied to moderator: %#v", event)
	}
}

func readEventType(t *testing.T, conn *websocket.Conn, eventType string) store.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, body, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event store.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == eventType {
			return event
		}
	}
}

func expectHTTPSeqs(t *testing.T, messages []store.Message, first, last int64) {
	t.Helper()
	wantLen := int(last - first + 1)
	if len(messages) != wantLen {
		t.Fatalf("expected %d messages from seq %d to %d, got %d: %#v", wantLen, first, last, len(messages), messages)
	}
	for i, message := range messages {
		want := first + int64(i)
		if message.ChannelSeq == nil || *message.ChannelSeq != want {
			t.Fatalf("message %d: expected seq %d, got %#v", i, want, message.ChannelSeq)
		}
	}
}
