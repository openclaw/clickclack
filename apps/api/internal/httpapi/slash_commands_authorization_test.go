package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type slashAuthorizationStore struct {
	store.Store
	persistedInvocations atomic.Int64
}

func (s *slashAuthorizationStore) CreateSlashCommandInvocation(ctx context.Context, input store.CreateSlashCommandInvocationInput) (store.SlashCommandInvocation, error) {
	invocation, err := s.Store.CreateSlashCommandInvocation(ctx, input)
	if err == nil {
		s.persistedInvocations.Add(1)
	}
	return invocation, err
}

func TestHTTPSlashCommandRequiresChannelWriteAuthorityBeforeCallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "http-slash-authz-moderator@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	createMember := func(name, email, role string) store.User {
		t.Helper()
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: name, Email: email})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, user.ID, role); err != nil {
			t.Fatal(err)
		}
		return user
	}
	member := createMember("Member", "http-slash-authz-member@example.com", store.WorkspaceRoleMember)
	blockedMember := createMember("Blocked Member", "http-slash-authz-blocked@example.com", store.WorkspaceRoleMember)
	timedMember := createMember("Timed Member", "http-slash-authz-timed@example.com", store.WorkspaceRoleMember)
	guest := createMember("Guest", "http-slash-authz-guest@example.com", store.WorkspaceRoleGuest)

	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generalChannelID, guestChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "general":
			generalChannelID = channel.ID
		case "guest":
			guestChannelID = channel.ID
		}
	}
	if generalChannelID == "" || guestChannelID == "" {
		t.Fatalf("expected general and guest channels, got %#v", channels)
	}
	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "HTTP Slash Other Workspace"}, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherGeneralChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: otherWorkspace.ID,
		Name:        "general",
		UserID:      moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherGeneralChannelID := otherGeneralChannel.ID

	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: blockedMember.ID,
		Blocked:      &blocked,
	}); err != nil {
		t.Fatal(err)
	}
	timeoutUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{
		WorkspaceID:  workspace.ID,
		ActorUserID:  moderator.ID,
		TargetUserID: timedMember.ID,
		TimeoutUntil: &timeoutUntil,
	}); err != nil {
		t.Fatal(err)
	}

	bot, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		DisplayName: "Slash Bot",
		Scopes:      []string{"messages:write"},
		CreatedBy:   moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var callbackCount atomic.Int64
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackCount.Add(1)
		if r.URL.Path == "/fail" {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "callback failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"response_type": "in_channel",
			"text":          "command accepted",
		})
	}))
	t.Cleanup(callback.Close)
	registeredCommand, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspace.ID,
		Command:     "/deploy",
		CallbackURL: callback.URL,
		BotUserID:   bot.ID,
		CreatedBy:   moderator.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSlashCommand(ctx, store.CreateSlashCommandInput{
		WorkspaceID: workspace.ID,
		Command:     "/broken",
		CallbackURL: callback.URL + "/fail",
		BotUserID:   bot.ID,
		CreatedBy:   moderator.ID,
	}); err != nil {
		t.Fatal(err)
	}

	trackedStore := &slashAuthorizationStore{Store: st}
	server := httptest.NewServer(New(trackedStore, realtime.NewHub(), Options{
		callbackClient: &http.Client{Timeout: callbackTimeout},
	}).Handler())
	t.Cleanup(server.Close)

	invoke := func(userID, bearerToken, channelID, command string) (int, map[string]any) {
		t.Helper()
		form := url.Values{"command": {command}, "text": {"prod"}}
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/hooks/slash/"+channelID, bytes.NewBufferString(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		} else {
			req.Header.Set("X-ClickClack-User", userID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatalf("decode slash response %s: %v", string(payload), err)
		}
		return resp.StatusCode, body
	}
	assertRegisteredSuccess := func(name, userID, token, channelID string) {
		t.Helper()
		beforeCallbacks := callbackCount.Load()
		beforeInvocations := trackedStore.persistedInvocations.Load()
		status, body := invoke(userID, token, channelID, "/deploy")
		if status != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d %#v", name, status, body)
		}
		message, _ := body["message"].(map[string]any)
		invocation, _ := body["invocation"].(map[string]any)
		if body["response_type"] != "in_channel" || body["text"] != "command accepted" || message["author_id"] != bot.ID || invocation["id"] == "" {
			t.Fatalf("%s: registered response shape changed: %#v", name, body)
		}
		if got := callbackCount.Load(); got != beforeCallbacks+1 {
			t.Fatalf("%s: callback count=%d, want %d", name, got, beforeCallbacks+1)
		}
		if got := trackedStore.persistedInvocations.Load(); got != beforeInvocations+1 {
			t.Fatalf("%s: persisted invocation count=%d, want %d", name, got, beforeInvocations+1)
		}
	}
	assertRegisteredFailure := func(name, userID, channelID string) {
		t.Helper()
		beforeCallbacks := callbackCount.Load()
		beforeInvocations := trackedStore.persistedInvocations.Load()
		status, body := invoke(userID, "", channelID, "/broken")
		if status != http.StatusBadGateway {
			t.Fatalf("%s: expected 502, got %d %#v", name, status, body)
		}
		if got := callbackCount.Load(); got != beforeCallbacks+1 {
			t.Fatalf("%s: callback count=%d, want %d", name, got, beforeCallbacks+1)
		}
		if got := trackedStore.persistedInvocations.Load(); got != beforeInvocations+1 {
			t.Fatalf("%s: failed callback did not consume a guest slot: count=%d, want %d", name, got, beforeInvocations+1)
		}
	}
	assertDenied := func(name, userID, channelID string, wantStatus int) {
		t.Helper()
		beforeCallbacks := callbackCount.Load()
		beforeInvocations := trackedStore.persistedInvocations.Load()
		status, body := invoke(userID, "", channelID, "/deploy")
		if status != wantStatus {
			t.Fatalf("%s: expected %d, got %d %#v", name, wantStatus, status, body)
		}
		if got := callbackCount.Load(); got != beforeCallbacks {
			t.Fatalf("%s reached callback: before=%d after=%d", name, beforeCallbacks, got)
		}
		if got := trackedStore.persistedInvocations.Load(); got != beforeInvocations {
			t.Fatalf("%s persisted invocation: before=%d after=%d", name, beforeInvocations, got)
		}
	}

	assertRegisteredSuccess("ordinary member", member.ID, "", generalChannelID)
	assertRegisteredSuccess("guest channel", guest.ID, "", guestChannelID)
	assertRegisteredSuccess("bot token", "", botToken.Token, generalChannelID)

	beforeCallbacks := callbackCount.Load()
	beforeInvocations := trackedStore.persistedInvocations.Load()
	status, body := invoke("", botToken.Token, otherGeneralChannelID, "/deploy")
	if status != http.StatusForbidden {
		t.Fatalf("cross-workspace bot invocation: expected 403, got %d %#v", status, body)
	}
	if callbackCount.Load() != beforeCallbacks || trackedStore.persistedInvocations.Load() != beforeInvocations {
		t.Fatal("cross-workspace bot invocation reached callback or persisted an invocation")
	}
	encodedBody, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{registeredCommand.ID, registeredCommand.CallbackURL, registeredCommand.SigningSecret} {
		if sensitive != "" && strings.Contains(string(encodedBody), sensitive) {
			t.Fatalf("cross-workspace error exposed registered command detail %q: %s", sensitive, encodedBody)
		}
	}

	beforeCallbacks = callbackCount.Load()
	beforeInvocations = trackedStore.persistedInvocations.Load()
	status, body = invoke(member.ID, "", generalChannelID, "/unregistered")
	message, _ := body["message"].(map[string]any)
	if status != http.StatusCreated || body["response_type"] != "in_channel" || body["text"] != "/unregistered prod" || message["author_id"] != member.ID {
		t.Fatalf("unregistered fallback changed: status=%d body=%#v", status, body)
	}
	if callbackCount.Load() != beforeCallbacks || trackedStore.persistedInvocations.Load() != beforeInvocations {
		t.Fatal("unregistered fallback called the registered command path")
	}

	assertDenied("guest hidden channel", guest.ID, generalChannelID, http.StatusForbidden)
	assertDenied("blocked member", blockedMember.ID, generalChannelID, http.StatusForbidden)
	assertDenied("timed-out member", timedMember.ID, generalChannelID, http.StatusForbidden)

	assertRegisteredFailure("failed guest callback", guest.ID, guestChannelID)
	assertRegisteredFailure("failed guest callback retry", guest.ID, guestChannelID)
	assertDenied("guest post budget", guest.ID, guestChannelID, http.StatusTooManyRequests)
}
