package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestHTTPBotCommandAuthorizationAndRealtime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, owner, member, outsider, workspace := newBotCommandHTTPStore(t)
	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Bot Commands"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	zetaBot, zetaToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Zeta Bot",
		Handle:      "zeta-bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	alphaBot, alphaToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Alpha Bot",
		Handle:      "alpha-bot",
		AvatarURL:   "https://example.com/alpha.png",
		Scopes:      []string{"bot:admin"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, noWriteToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Read Only Bot",
		Handle:      "read-only-bot",
		Scopes:      []string{"workspaces:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, writeOnlyToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Write Only Bot",
		Handle:      "write-only-bot",
		Scopes:      []string{store.BotCommandsWriteScope},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, otherWorkspace.ID, zetaBot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetBotCommands(ctx, otherWorkspace.ID, zetaBot.ID, []store.BotCommandInput{
		{Command: "other", Description: "Other workspace command"},
	}); err != nil {
		t.Fatal(err)
	}

	hub := realtime.NewHub()
	events, unsubscribe := hub.Subscribe(workspace.ID)
	t.Cleanup(unsubscribe)
	server := httptest.NewServer(New(st, hub, Options{}).Handler())
	t.Cleanup(server.Close)

	endpoint := server.URL + "/api/bots/self/commands"
	expectStatusAsUser(t, owner.ID, http.MethodPut, endpoint, strings.NewReader(`{"commands":[]}`), http.StatusForbidden)
	expectStatusWithBearer(t, noWriteToken.Token, http.MethodPut, endpoint, strings.NewReader(`{"commands":[]}`), http.StatusForbidden)
	expectStatusWithBearer(t, zetaToken.Token, http.MethodPut, endpoint, strings.NewReader(`{}`), http.StatusBadRequest)

	setResult, status := putJSONWithBearerStatus[struct {
		BotCommands []store.BotCommand `json:"bot_commands"`
	}](t, zetaToken.Token, endpoint, map[string]any{
		"workspace_id": otherWorkspace.ID,
		"bot_user_id":  alphaBot.ID,
		"commands": []map[string]string{
			{"command": "status", "description": "Show status"},
			{"command": "new", "description": "Start a session", "args_hint": "[message]"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("expected bot command update to succeed, got %d", status)
	}
	if len(setResult.BotCommands) != 2 || setResult.BotCommands[0].Command != "/new" || setResult.BotCommands[1].Command != "/status" {
		t.Fatalf("unexpected bot command response: %#v", setResult.BotCommands)
	}
	for _, command := range setResult.BotCommands {
		if command.WorkspaceID != workspace.ID || command.BotUserID != zetaBot.ID {
			t.Fatalf("request body overrode token identity or workspace: %#v", command)
		}
	}
	otherCommands, err := st.ListBotCommands(ctx, otherWorkspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherCommands) != 1 || otherCommands[0].Command != "/other" {
		t.Fatalf("workspace-bound token changed another workspace: %#v", otherCommands)
	}

	select {
	case event := <-events:
		if event.Type != "bot_command.updated" || event.WorkspaceID != workspace.ID || event.ID != "" || event.Cursor != "" || store.IsDurableEventType(event.Type) {
			t.Fatalf("unexpected bot command realtime event: %#v", event)
		}
		payload, ok := event.Payload.(map[string]string)
		if !ok || payload["workspace_id"] != workspace.ID || payload["bot_user_id"] != zetaBot.ID {
			t.Fatalf("unexpected bot command realtime payload: %#v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bot command realtime event")
	}

	alphaResult, status := putJSONWithBearerStatus[struct {
		BotCommands []store.BotCommand `json:"bot_commands"`
	}](t, alphaToken.Token, endpoint, map[string]any{
		"commands": []map[string]string{{"command": "about", "description": "About this agent"}},
	})
	if status != http.StatusOK || len(alphaResult.BotCommands) != 1 {
		t.Fatalf("unexpected second bot update: status=%d body=%#v", status, alphaResult)
	}

	memberList := getBotCommandsAsUser(t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands")
	if len(memberList) != 3 {
		t.Fatalf("expected commands from both populated bots, got %#v", memberList)
	}
	if memberList[0].Bot.Handle != "alpha-bot" || memberList[0].Command != "/about" ||
		memberList[1].Bot.Handle != "zeta-bot" || memberList[1].Command != "/new" ||
		memberList[2].Command != "/status" {
		t.Fatalf("expected handle-then-command sorting, got %#v", memberList)
	}
	if memberList[0].Bot.ID != alphaBot.ID || memberList[0].Bot.DisplayName != "Alpha Bot" || memberList[0].Bot.AvatarURL != "https://example.com/alpha.png" {
		t.Fatalf("expected embedded bot identity, got %#v", memberList[0].Bot)
	}

	botList, status := getJSONWithBearerStatus[struct {
		BotCommands []store.WorkspaceBotCommand `json:"bot_commands"`
	}](t, zetaToken.Token, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands")
	if status != http.StatusOK || len(botList.BotCommands) != 3 {
		t.Fatalf("expected bound bot with workspaces:read to list commands: status=%d body=%#v", status, botList)
	}
	expectStatusWithBearer(t, writeOnlyToken.Token, http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands", nil, http.StatusForbidden)
	expectStatusWithBearer(t, zetaToken.Token, http.MethodGet, server.URL+"/api/workspaces/"+otherWorkspace.ID+"/bot-commands", nil, http.StatusForbidden)
	expectStatusAsUser(t, outsider.ID, http.MethodGet, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands", nil, http.StatusForbidden)

	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		TargetUserID: zetaBot.ID,
		ActorUserID:  owner.ID,
		Role:         store.WorkspaceRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	expectStatusWithBearer(t, zetaToken.Token, http.MethodPut, endpoint, strings.NewReader(`{"commands":[]}`), http.StatusForbidden)
	if err := st.RemoveBotFromWorkspace(ctx, workspace.ID, zetaBot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	memberList = getBotCommandsAsUser(t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands")
	if len(memberList) != 1 || memberList[0].Bot.ID != alphaBot.ID {
		t.Fatalf("expected removal cleanup in list response, got %#v", memberList)
	}
}

func TestHTTPBotCommandOverwriteClearAndValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, owner, member, _, workspace := newBotCommandHTTPStore(t)
	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Validation Bot",
		Handle:      "validation-bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherBot, otherToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Other Bot",
		Handle:      "other-bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	endpoint := server.URL + "/api/bots/self/commands"

	if _, status := putJSONWithBearerStatus[struct{}](t, otherToken.Token, endpoint, map[string]any{
		"commands": []map[string]string{{"command": "other", "description": "Other command"}},
	}); status != http.StatusOK {
		t.Fatalf("expected second bot setup to succeed, got %d", status)
	}
	initial, status := putJSONWithBearerStatus[struct {
		BotCommands []store.BotCommand `json:"bot_commands"`
	}](t, token.Token, endpoint, map[string]any{
		"commands": []map[string]string{
			{"command": "status", "description": "Status"},
			{"command": "new", "description": "New session"},
		},
	})
	if status != http.StatusOK || len(initial.BotCommands) != 2 {
		t.Fatalf("unexpected initial command response: status=%d body=%#v", status, initial)
	}
	replaced, status := putJSONWithBearerStatus[struct {
		BotCommands []store.BotCommand `json:"bot_commands"`
	}](t, token.Token, endpoint, map[string]any{
		"commands": []map[string]string{{"command": "help", "description": "Help"}},
	})
	if status != http.StatusOK || len(replaced.BotCommands) != 1 || replaced.BotCommands[0].Command != "/help" {
		t.Fatalf("expected overwrite to replace the full set: status=%d body=%#v", status, replaced)
	}

	tooMany := make([]map[string]string, 101)
	for i := range tooMany {
		tooMany[i] = map[string]string{"command": "cmd", "description": "valid"}
	}
	validationCases := []struct {
		name string
		body map[string]any
	}{
		{"bad name", map[string]any{"commands": []map[string]string{{"command": "bad name", "description": "invalid"}}}},
		{"missing description", map[string]any{"commands": []map[string]string{{"command": "status"}}}},
		{"too many", map[string]any{"commands": tooMany}},
		{"duplicate", map[string]any{"commands": []map[string]string{
			{"command": "status", "description": "first"},
			{"command": "/STATUS", "description": "second"},
		}}},
	}
	for _, tc := range validationCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, status := putJSONWithBearerStatus[struct{}](t, token.Token, endpoint, tc.body); status != http.StatusBadRequest {
				t.Fatalf("expected validation failure, got %d", status)
			}
			listed := getBotCommandsAsUser(t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands")
			if !slices.ContainsFunc(listed, func(command store.WorkspaceBotCommand) bool {
				return command.Bot.ID == bot.ID && command.Command == "/help"
			}) {
				t.Fatalf("validation changed the prior menu: %#v", listed)
			}
			if !slices.ContainsFunc(listed, func(command store.WorkspaceBotCommand) bool {
				return command.Bot.ID == otherBot.ID && command.Command == "/other"
			}) {
				t.Fatalf("validation changed the other bot's menu: %#v", listed)
			}
		})
	}

	cleared, status := putJSONWithBearerStatus[struct {
		BotCommands []store.BotCommand `json:"bot_commands"`
	}](t, token.Token, endpoint, map[string]any{"commands": []any{}})
	if status != http.StatusOK || len(cleared.BotCommands) != 0 {
		t.Fatalf("expected empty array to clear the menu: status=%d body=%#v", status, cleared)
	}
	listed := getBotCommandsAsUser(t, member.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bot-commands")
	if len(listed) != 1 || listed[0].Bot.ID != otherBot.ID || listed[0].Command != "/other" {
		t.Fatalf("clear affected another bot or left stale commands: %#v", listed)
	}
}

func newBotCommandHTTPStore(t *testing.T) (*sqlitestore.Store, store.User, store.User, store.User, store.Workspace) {
	t.Helper()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Bot Command Owner", "http-bot-command-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Command Member", Email: "http-bot-command-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bot Command Outsider", Email: "http-bot-command-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return st, owner, member, outsider, workspace
}

func getBotCommandsAsUser(t *testing.T, userID, endpoint string) []store.WorkspaceBotCommand {
	t.Helper()
	result := getJSONAsUser[struct {
		BotCommands []store.WorkspaceBotCommand `json:"bot_commands"`
	}](t, userID, endpoint)
	return result.BotCommands
}

func putJSONWithBearerStatus[T any](t *testing.T, token, endpoint string, body any) (T, int) {
	t.Helper()
	return requestJSONWithBearerStatus[T](t, token, http.MethodPut, endpoint, body)
}

func getJSONWithBearerStatus[T any](t *testing.T, token, endpoint string) (T, int) {
	t.Helper()
	return requestJSONWithBearerStatus[T](t, token, http.MethodGet, endpoint, nil)
}

func requestJSONWithBearerStatus[T any](t *testing.T, token, method, endpoint string, body any) (T, int) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, endpoint, reader)
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
	var out T
	if resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, endpoint, err)
		}
	}
	return out, resp.StatusCode
}
