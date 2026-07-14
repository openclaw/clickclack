package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
	"github.com/openclaw/clickclack/apps/api/internal/webassets"
)

func TestHTTPUnauthorizedRoutes(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
	t.Cleanup(server.Close)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/me", ""},
		{http.MethodGet, "/api/event-types", ""},
		{http.MethodGet, "/api/workspaces", ""},
		{http.MethodPost, "/api/workspaces", `{"name":"x"}`},
		{http.MethodGet, "/api/routes/TMISSING/CMISSING", ""},
		{http.MethodGet, "/api/workspaces/wsp_missing", ""},
		{http.MethodGet, "/api/workspaces/wsp_missing/channels", ""},
		{http.MethodPost, "/api/workspaces/wsp_missing/channels", `{"name":"x"}`},
		{http.MethodGet, "/api/channels/chn_missing/messages", ""},
		{http.MethodPost, "/api/channels/chn_missing/messages", `{"body":"x"}`},
		{http.MethodGet, "/api/messages/msg_missing", ""},
		{http.MethodGet, "/api/messages/msg_missing/thread", ""},
		{http.MethodPost, "/api/messages/msg_missing/thread/replies", `{"body":"x"}`},
		{http.MethodPost, "/api/messages/msg_missing/reactions", `{"emoji":"x"}`},
		{http.MethodDelete, "/api/messages/msg_missing/reactions/x", ""},
		{http.MethodGet, "/api/realtime/events?workspace_id=wsp_missing", ""},
		{http.MethodGet, "/api/realtime/ws?workspace_id=wsp_missing", ""},
		{http.MethodGet, "/api/search?workspace_id=wsp_missing&q=x", ""},
		{http.MethodPost, "/api/uploads", ""},
		{http.MethodGet, "/api/uploads/by-nonce?workspace_id=wsp_missing&nonce=missing", ""},
		{http.MethodGet, "/api/messages/by-nonce?workspace_id=wsp_missing&nonce=missing", ""},
		{http.MethodGet, "/api/uploads/upl_missing", ""},
		{http.MethodPost, "/api/messages/msg_missing/attachments", `{"upload_id":"upl_missing"}`},
		{http.MethodGet, "/api/dms?workspace_id=wsp_missing", ""},
		{http.MethodPost, "/api/dms", `{"workspace_id":"wsp_missing","member_ids":[]}`},
		{http.MethodGet, "/api/dms/dm_missing/messages", ""},
		{http.MethodPost, "/api/dms/dm_missing/messages", `{"body":"x"}`},
		{http.MethodPost, "/api/hooks/mattermost/chn_missing", `{"text":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected unauthorized, got %s %s", resp.Status, string(body))
			}
		})
	}
}

func TestShouldDeliverPrivateEvents(t *testing.T) {
	t.Parallel()

	if !shouldDeliverEvent(store.Event{Type: "message.created"}, "usr_owner") {
		t.Fatal("expected public event to be delivered")
	}
	if !shouldDeliverEvent(store.Event{Type: "typing.started", RecipientUserIDs: []string{"usr_owner", "usr_other"}}, "usr_other") {
		t.Fatal("expected targeted event to be delivered to recipient")
	}
	if shouldDeliverEvent(store.Event{Type: "typing.started", RecipientUserIDs: []string{"usr_owner"}}, "usr_other") {
		t.Fatal("expected targeted event to be hidden from non-recipient")
	}
	if !shouldDeliverEvent(store.Event{Type: "dm.read", Payload: map[string]string{"user_id": "usr_owner"}}, "usr_owner") {
		t.Fatal("expected read receipt to be delivered to owner")
	}
	if shouldDeliverEvent(store.Event{Type: "dm.read", Payload: map[string]string{"user_id": "usr_owner"}}, "usr_other") {
		t.Fatal("expected read receipt to be hidden from other user")
	}
	if !shouldDeliverEvent(store.Event{Type: "channel.read", Payload: map[string]any{"user_id": "usr_owner"}}, "usr_owner") {
		t.Fatal("expected backlog read receipt to be delivered to owner")
	}
	if shouldDeliverEvent(store.Event{Type: "channel.read", Payload: map[string]any{}}, "usr_owner") {
		t.Fatal("expected malformed backlog read receipt to be hidden")
	}
	if shouldDeliverEvent(store.Event{Type: "channel.read", Payload: "bad"}, "usr_owner") {
		t.Fatal("expected malformed read receipt to be hidden")
	}
}

func TestHTTPBotTokenWorkspaceIsolation(t *testing.T) {
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
	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: otherWorkspace.ID, UserID: owner.ID, Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: otherChannel.ID, AuthorID: owner.ID, Body: "other workspace message"})
	if err != nil {
		t.Fatal(err)
	}
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Workspace Locked Bot",
		Scopes:      []string{"bot:admin"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, otherWorkspace.ID, bot.ID, "bot"); err != nil {
		t.Fatal(err)
	}
	otherDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: otherWorkspace.ID, UserID: owner.ID, MemberIDs: []string{bot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	otherDMMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: otherDM.ID, AuthorID: owner.ID, Body: "other dm"})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/workspaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected scoped workspace list, got %s %s", resp.Status, string(body))
	}
	var workspaceList struct {
		Workspaces []store.Workspace `json:"workspaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&workspaceList); err != nil {
		t.Fatal(err)
	}
	if len(workspaceList.Workspaces) != 1 || workspaceList.Workspaces[0].ID != workspace.ID {
		t.Fatalf("bot token listed workspaces outside token scope: %#v", workspaceList.Workspaces)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	ownDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{bot.ID}})
	if err != nil {
		t.Fatal(err)
	}
	expectStatusWithBearer(t, token.Token, http.MethodPatch, server.URL+"/api/channels/"+channels[0].ID, strings.NewReader(`{"name":"bot-updated"}`), http.StatusOK)
	expectStatusWithBearer(t, token.Token, http.MethodDelete, server.URL+"/api/dms/"+ownDM.ID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/dms/"+ownDM.ID+"/open", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPatch, server.URL+"/api/me", strings.NewReader(`{"display_name":"Bot"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/workspaces", strings.NewReader(`{"name":"Nope"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/workspaces/"+otherWorkspace.ID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/workspaces/"+otherWorkspace.ID+"/channels", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/workspaces/"+otherWorkspace.ID+"/channels", strings.NewReader(`{"name":"hidden"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/search?workspace_id="+otherWorkspace.ID+"&q=scope", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/realtime/events?workspace_id="+otherWorkspace.ID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/dms", strings.NewReader(`{"workspace_id":"`+otherWorkspace.ID+`","member_ids":[]}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/routes/"+otherWorkspace.RouteID+"/"+otherChannel.RouteID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/channels/"+otherChannel.ID+"/messages", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/channels/"+otherChannel.ID+"/messages", strings.NewReader(`{"body":"nope"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/channels/"+otherChannel.ID+"/read", strings.NewReader(`{"seq":1}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/hooks/mattermost/"+otherChannel.ID, strings.NewReader(`{"text":"nope"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/hooks/slash/"+otherChannel.ID, strings.NewReader(`command=/nope`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/messages/"+otherMessage.ID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/messages/"+otherMessage.ID+"/thread", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/messages/"+otherMessage.ID+"/thread/replies", strings.NewReader(`{"body":"nope"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/messages/"+otherMessage.ID+"/reactions", strings.NewReader(`{"emoji":"x"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodDelete, server.URL+"/api/messages/"+otherMessage.ID+"/reactions/x", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/dms/"+otherDM.ID+"/messages", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodDelete, server.URL+"/api/dms/"+otherDM.ID, nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/dms/"+otherDM.ID+"/open", nil, http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/dms/"+otherDM.ID+"/messages", strings.NewReader(`{"body":"nope"}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodPost, server.URL+"/api/dms/"+otherDM.ID+"/read", strings.NewReader(`{"seq":1}`), http.StatusForbidden)
	expectStatusWithBearer(t, token.Token, http.MethodGet, server.URL+"/api/messages/"+otherDMMessage.ID, nil, http.StatusForbidden)

	_, profileToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Profile Only Bot",
		Scopes:      []string{"profile:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectStatusWithBearer(t, profileToken.Token, http.MethodGet, server.URL+"/api/messages/"+otherMessage.ID, nil, http.StatusForbidden)
}

func TestHTTPBotManagementAuthorization(t *testing.T) {
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
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "mod-bot-authz@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-bot-authz@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	botOwner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Owner", Email: "bot-owner-authz@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, botOwner.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/bots", strings.NewReader(`{"display_name":"member service bot"}`), http.StatusForbidden)
	serviceBot := postJSONAsUser[struct {
		Bot      store.User     `json:"bot"`
		BotToken store.BotToken `json:"bot_token"`
	}](t, moderator.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots", map[string]any{
		"display_name": "service bot",
		"token_name":   "initial",
		"scopes":       []string{"bot:read"},
	})
	if serviceBot.Bot.OwnerUserID != "" || serviceBot.BotToken.Token == "" {
		t.Fatalf("unexpected service bot payload: %#v", serviceBot)
	}
	serviceTokens := getJSONAsUser[struct {
		BotTokens []store.BotToken `json:"bot_tokens"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+serviceBot.Bot.ID+"/tokens")
	if len(serviceTokens.BotTokens) != 1 || serviceTokens.BotTokens[0].Token != "" {
		t.Fatalf("expected member to see redacted service token metadata, got %#v", serviceTokens.BotTokens)
	}
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+serviceBot.Bot.ID+"/tokens", strings.NewReader(`{"name":"member rotate"}`), http.StatusForbidden)
	serviceRotation := postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+serviceBot.Bot.ID+"/tokens", map[string]any{"name": "owner rotate"})
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/bot-tokens/"+serviceRotation.BotToken.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)
	postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, moderator.ID, server.URL+"/api/bot-tokens/"+serviceRotation.BotToken.ID+"/revoke", map[string]any{})

	expectStatusAsUser(t, moderator.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/bots", strings.NewReader(`{"owner_user_id":"`+botOwner.ID+`","display_name":"manager-owned attempt"}`), http.StatusForbidden)
	userBot := postJSONAsUser[struct {
		Bot      store.User     `json:"bot"`
		BotToken store.BotToken `json:"bot_token"`
	}](t, botOwner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots", map[string]any{
		"owner_user_id": botOwner.ID,
		"display_name":  "user owned bot",
		"token_name":    "initial",
		"scopes":        []string{"bot:read"},
	})
	if userBot.Bot.OwnerUserID != botOwner.ID {
		t.Fatalf("expected user-owned bot, got %#v", userBot.Bot)
	}
	userTokens := getJSONAsUser[struct {
		BotTokens []store.BotToken `json:"bot_tokens"`
	}](t, moderator.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+userBot.Bot.ID+"/tokens")
	if len(userTokens.BotTokens) != 1 || userTokens.BotTokens[0].Token != "" {
		t.Fatalf("expected manager to see redacted user-owned token metadata, got %#v", userTokens.BotTokens)
	}
	expectStatusAsUser(t, moderator.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+userBot.Bot.ID+"/tokens", strings.NewReader(`{"name":"manager rotate"}`), http.StatusForbidden)
	ownerRotation := postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, botOwner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+userBot.Bot.ID+"/tokens", map[string]any{"name": "owner rotate"})
	expectStatusAsUser(t, moderator.ID, http.MethodPost, server.URL+"/api/bot-tokens/"+ownerRotation.BotToken.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)
	postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, botOwner.ID, server.URL+"/api/bot-tokens/"+ownerRotation.BotToken.ID+"/revoke", map[string]any{})

	myBots := getJSONAsUser[struct {
		Bots []store.OwnedBotEntry `json:"bots"`
	}](t, botOwner.ID, server.URL+"/api/me/bots")
	if len(myBots.Bots) != 1 || myBots.Bots[0].Bot.ID != userBot.Bot.ID || myBots.Bots[0].Workspace.ID != workspace.ID || myBots.Bots[0].ActiveTokenCount != 1 {
		t.Fatalf("unexpected owned bot list: %#v", myBots.Bots)
	}
	ownerBots := getJSONAsUser[struct {
		Bots []store.OwnedBotEntry `json:"bots"`
	}](t, owner.ID, server.URL+"/api/me/bots")
	if len(ownerBots.Bots) != 0 {
		t.Fatalf("service bots must not be listed as owned bots: %#v", ownerBots.Bots)
	}

	expectStatusAsUser(t, member.ID, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+serviceBot.Bot.ID+"/membership", nil, http.StatusForbidden)
	expectStatusAsUser(t, moderator.ID, http.MethodDelete, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+serviceBot.Bot.ID+"/membership", nil, http.StatusNoContent)
	if _, err := st.GetBotTokenAuth(ctx, serviceBot.BotToken.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected removed service bot token to stop authenticating, got %v", err)
	}
}

func TestHTTPIntegrationManagementAuthorization(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "integration-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "integration-moderator@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "integration-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Integration Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	eventTypes := getJSONAsUser[struct {
		EventTypes []string `json:"event_types"`
	}](t, member.ID, server.URL+"/api/event-types")
	if !slices.Equal(eventTypes.EventTypes, store.DurableEventTypes) {
		t.Fatalf("unexpected durable event types: %#v", eventTypes.EventTypes)
	}
	expectStatusWithBearer(t, botToken.Token, http.MethodGet, server.URL+"/api/event-types", nil, http.StatusOK)

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations", strings.NewReader(`{"app_slug":"blocked","bot_user_id":"`+bot.ID+`"}`), http.StatusForbidden)
	installation := postJSONAsUser[struct {
		AppInstallation store.AppInstallation `json:"app_installation"`
	}](t, moderator.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations", map[string]any{
		"app_slug":    "openclaw",
		"bot_user_id": bot.ID,
	})
	getJSONAsUser[struct {
		AppInstallations []store.AppInstallation `json:"app_installations"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations")
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/app-installations/"+installation.AppInstallation.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands", strings.NewReader(`{"command":"/blocked","callback_url":"https://example.com/slash","bot_user_id":"`+bot.ID+`"}`), http.StatusForbidden)
	command := postJSONAsUser[struct {
		SlashCommand store.SlashCommand `json:"slash_command"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands", map[string]any{
		"app_installation_id": installation.AppInstallation.ID,
		"command":             "/deploy",
		"callback_url":        "https://example.com/slash",
		"bot_user_id":         bot.ID,
	})
	getJSONAsUser[struct {
		SlashCommands []store.SlashCommand `json:"slash_commands"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands")
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/slash-commands/"+command.SlashCommand.ID+"/rotate-secret", strings.NewReader(`{}`), http.StatusForbidden)
	rotatedCommand := postJSONAsUser[struct {
		SlashCommand store.SlashCommand `json:"slash_command"`
	}](t, moderator.ID, server.URL+"/api/slash-commands/"+command.SlashCommand.ID+"/rotate-secret", map[string]any{})
	if rotatedCommand.SlashCommand.ID != command.SlashCommand.ID || rotatedCommand.SlashCommand.SigningSecret == "" || rotatedCommand.SlashCommand.SigningSecret == command.SlashCommand.SigningSecret {
		t.Fatalf("unexpected rotated slash command: %#v", rotatedCommand.SlashCommand)
	}
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/slash-commands/"+command.SlashCommand.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions", strings.NewReader(`{"event_types":["message.created"],"callback_url":"https://example.com/events"}`), http.StatusForbidden)
	subscription := postJSONAsUser[struct {
		EventSubscription store.EventSubscription `json:"event_subscription"`
	}](t, moderator.ID, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions", map[string]any{
		"app_installation_id": installation.AppInstallation.ID,
		"event_types":         []string{"message.created"},
		"callback_url":        "https://example.com/events",
	})
	getJSONAsUser[struct {
		EventSubscriptions []store.EventSubscription `json:"event_subscriptions"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions")
	expectStatusAsUser(t, owner.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions", strings.NewReader(`{"event_types":["message.typo"],"callback_url":"https://example.com/events"}`), http.StatusBadRequest)
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/event-subscriptions/"+subscription.EventSubscription.ID+"/rotate-secret", strings.NewReader(`{}`), http.StatusForbidden)
	rotatedSubscription := postJSONAsUser[struct {
		EventSubscription store.EventSubscription `json:"event_subscription"`
	}](t, owner.ID, server.URL+"/api/event-subscriptions/"+subscription.EventSubscription.ID+"/rotate-secret", map[string]any{})
	if rotatedSubscription.EventSubscription.ID != subscription.EventSubscription.ID || rotatedSubscription.EventSubscription.SigningSecret == "" || rotatedSubscription.EventSubscription.SigningSecret == subscription.EventSubscription.SigningSecret {
		t.Fatalf("unexpected rotated event subscription: %#v", rotatedSubscription.EventSubscription)
	}
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/event-subscriptions/"+subscription.EventSubscription.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)

	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts", strings.NewReader(`{"user_id":"`+member.ID+`","provider":"github","provider_account_id":"blocked"}`), http.StatusForbidden)
	memberAccount := postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts", map[string]any{
		"user_id":             member.ID,
		"provider":            "github",
		"provider_account_id": "member",
	})
	postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, member.ID, server.URL+"/api/connected-accounts/"+memberAccount.ConnectedAccount.ID+"/revoke", map[string]any{})

	ownerAccount := postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, moderator.ID, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts", map[string]any{
		"user_id":             owner.ID,
		"provider":            "github",
		"provider_account_id": "owner",
	})
	getJSONAsUser[struct {
		ConnectedAccounts []store.ConnectedAccount `json:"connected_accounts"`
	}](t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/connected-accounts")
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/connected-accounts/"+ownerAccount.ConnectedAccount.ID+"/revoke", strings.NewReader(`{}`), http.StatusForbidden)
	postJSONAsUser[struct {
		ConnectedAccount store.ConnectedAccount `json:"connected_account"`
	}](t, moderator.ID, server.URL+"/api/connected-accounts/"+ownerAccount.ConnectedAccount.ID+"/revoke", map[string]any{})
}

func TestHTTPAppInstallationRevokeCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "installation-http-cascade@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	bot, initialToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Installation HTTP Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
	t.Cleanup(server.Close)

	first := postJSONAsUser[struct {
		AppInstallation store.AppInstallation `json:"app_installation"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations", map[string]any{
		"app_slug":    "cascade-defaults",
		"bot_user_id": bot.ID,
	})
	postJSONAsUser[struct {
		SlashCommand store.SlashCommand `json:"slash_command"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/slash-commands", map[string]any{
		"app_installation_id": first.AppInstallation.ID,
		"command":             "/cascade-defaults",
		"callback_url":        "https://example.com/slash",
		"bot_user_id":         bot.ID,
	})
	postJSONAsUser[struct {
		EventSubscription store.EventSubscription `json:"event_subscription"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/event-subscriptions", map[string]any{
		"app_installation_id": first.AppInstallation.ID,
		"event_types":         []string{"message.created"},
		"callback_url":        "https://example.com/events",
	})

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/app-installations/"+first.AppInstallation.ID+"/revoke", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected absent-body revoke response: %s %s", resp.Status, string(body))
	}
	var defaultResult store.RevokeAppInstallationResult
	if err := json.NewDecoder(resp.Body).Decode(&defaultResult); err != nil {
		t.Fatal(err)
	}
	if defaultResult.Installation.RevokedAt == nil || defaultResult.Revoked.SlashCommands != 1 || defaultResult.Revoked.EventSubscriptions != 1 || defaultResult.Revoked.BotTokens != 0 {
		t.Fatalf("unexpected absent-body cascade result: %#v", defaultResult)
	}
	if _, err := st.GetBotTokenAuth(ctx, initialToken.Token); err != nil {
		t.Fatalf("default cascade revoked the bot token: %v", err)
	}

	second := postJSONAsUser[struct {
		AppInstallation store.AppInstallation `json:"app_installation"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/app-installations", map[string]any{
		"app_slug":    "cascade-tokens",
		"bot_user_id": bot.ID,
	})
	secondToken, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{
		WorkspaceID: workspace.ID,
		BotUserID:   bot.ID,
		Name:        "second",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	tokenResult := postJSONAsUser[store.RevokeAppInstallationResult](t, owner.ID, server.URL+"/api/app-installations/"+second.AppInstallation.ID+"/revoke", map[string]any{
		"revoke_slash_commands":      false,
		"revoke_event_subscriptions": false,
		"revoke_bot_tokens":          true,
	})
	if tokenResult.Revoked.SlashCommands != 0 || tokenResult.Revoked.EventSubscriptions != 0 || tokenResult.Revoked.BotTokens != 2 {
		t.Fatalf("unexpected token cascade result: %#v", tokenResult)
	}
	for _, token := range []string{initialToken.Token, secondToken.Token} {
		if _, err := st.GetBotTokenAuth(ctx, token); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected cascaded bot token to stop authenticating, got %v", err)
		}
	}
	repeated := postJSONAsUser[store.RevokeAppInstallationResult](t, owner.ID, server.URL+"/api/app-installations/"+second.AppInstallation.ID+"/revoke", map[string]any{
		"revoke_slash_commands":      true,
		"revoke_event_subscriptions": true,
		"revoke_bot_tokens":          true,
	})
	if repeated.Revoked != (store.AppInstallationRevokedCounts{}) {
		t.Fatalf("expected repeated revoke counts to be zero, got %#v", repeated.Revoked)
	}
}

func TestHTTPUploadNotConfiguredAndCookieAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	link, err := st.CreateMagicLink(ctx, "cookie@example.com", "Cookie User")
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := st.ConsumeMagicLink(ctx, link.Token)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected cookie auth, got %s", resp.Status)
	}
	resp.Body.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("workspace_id", "unused"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodPost, server.URL+"/api/uploads", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-ClickClack-User", owner.ID)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected upload config error, got %s", resp.Status)
	}
}

func TestCookieSessionMutationsRequireCSRF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(st, realtime.NewHub(), Options{DisableDevAuth: true}).Handler()

	req := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected safe cookie request to pass, got %d: %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "http://chat.example.com/api/me", strings.NewReader(`{"display_name":"No CSRF"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://chat.example.com")
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected missing csrf header to fail, got %d: %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "http://chat.example.com/api/me", strings.NewReader(`{"display_name":"Cross Site"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, "1")
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected cross-site csrf request to fail, got %d: %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "http://chat.example.com/api/me", strings.NewReader(`{"display_name":"Same Origin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, "1")
	req.Header.Set("Origin", "http://chat.example.com")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected same-origin csrf request to pass, got %d: %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "http://chat.example.com/api/me", strings.NewReader(`{"display_name":"Bearer"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Origin", "https://evil.example")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected bearer mutation to bypass cookie csrf, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCanonicalOriginNormalizesAndValidatesPorts(t *testing.T) {
	t.Parallel()
	for input, expected := range map[string]string{
		"https://chat.example.com:0443":  "https://chat.example.com",
		"https://chat.example.com:08443": "https://chat.example.com:8443",
		"http://127.0.0.1:080":           "http://127.0.0.1",
	} {
		value, err := url.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := canonicalOrigin(value)
		if !ok || got != expected {
			t.Fatalf("canonical origin %q: got %q, %v; want %q", input, got, ok, expected)
		}
	}
	for _, input := range []string{"https://chat.example.com:0", "https://chat.example.com:65536"} {
		value, err := url.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := canonicalOrigin(value); ok {
			t.Fatalf("expected invalid origin %q to fail, got %q", input, got)
		}
	}
}

func TestHTTPMalformedJSONRoutes(t *testing.T) {
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
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "root"})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	paths := []string{
		"/api/auth/magic/request",
		"/api/auth/magic/consume",
		"/api/workspaces",
		"/api/workspaces/" + workspaces[0].ID + "/channels",
		"/api/channels/" + channels[0].ID + "/messages",
		"/api/messages/" + root.ID + "/thread/replies",
		"/api/messages/" + root.ID + "/reactions",
		"/api/messages/" + root.ID + "/attachments",
		"/api/dms",
		"/api/hooks/mattermost/" + channels[0].ID,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader("{"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected bad request, got %s %s", resp.Status, string(body))
			}
		})
	}

	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspaces[0].ID, UserID: owner.ID, MemberIDs: []string{other.ID}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/dms/"+dm.ID+"/messages", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected malformed dm message error, got %s", resp.Status)
	}
	resp.Body.Close()

	{
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "early.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("early")); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("workspace_id", workspaces[0].ID); err != nil {
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
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected file-before-workspace upload to be bad request, got %s", resp.Status)
		}
	}

	resp, err = http.PostForm(server.URL+"/api/hooks/slash/"+channels[0].ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected empty slash command error, got %s", resp.Status)
	}
}

func TestHTTPServesEmbeddedAsset(t *testing.T) {
	t.Parallel()
	st := newEmptyHTTPStore(t)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	var assetPath string
	if err := fs.WalkDir(webassets.Dist, "dist/_app/immutable", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		assetPath = strings.TrimPrefix(path, "dist/")
		return fs.SkipAll
	}); err != nil {
		t.Fatal(err)
	}
	if assetPath == "" {
		t.Fatal("expected embedded SvelteKit assets")
	}
	resp, err := http.Get(server.URL + "/" + assetPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected asset response, got %s", resp.Status)
	}

	deepResp, err := http.Get(server.URL + "/app/wsp_missing/chn_missing")
	if err != nil {
		t.Fatal(err)
	}
	defer deepResp.Body.Close()
	if deepResp.StatusCode != http.StatusOK {
		t.Fatalf("expected deep app route fallback, got %s", deepResp.Status)
	}
	body, err := io.ReadAll(deepResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("_app/immutable")) {
		t.Fatal("expected SvelteKit fallback HTML")
	}
}

func TestListenAndServeStopsWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ListenAndServe(ctx, "127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func newEmptyHTTPStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func newHTTPStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st := newEmptyHTTPStore(t)
	_, _ = st.CreateUser(context.Background(), store.CreateUserInput{DisplayName: "seed", Email: "seed@example.com"})
	return st
}
