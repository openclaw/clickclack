package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func newSetupCodeTestServer(t *testing.T) (*httptest.Server, store.Store, store.Workspace, store.User, store.User) {
	t.Helper()
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
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-setup-codes@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	return server, st, workspace, owner, member
}

func TestHTTPBotSetupCodeMintAndClaim(t *testing.T) {
	t.Parallel()
	server, _, workspace, owner, member := newSetupCodeTestServer(t)

	bot := postJSONAsUser[struct {
		Bot store.User `json:"bot"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots", map[string]any{
		"display_name": "setup code bot",
	})

	mintURL := server.URL + "/api/workspaces/" + workspace.ID + "/bots/" + bot.Bot.ID + "/setup-codes"

	// Mint requires manager rights (dev-auth test servers resolve
	// anonymous local requests, so 401 is covered by authz_test patterns).
	expectStatusAsUser(t, member.ID, http.MethodPost, mintURL, strings.NewReader(`{"name":"gateway"}`), http.StatusForbidden)

	minted := postJSONAsUser[struct {
		SetupCode store.BotSetupCode `json:"setup_code"`
	}](t, owner.ID, mintURL, map[string]any{
		"name":   "gateway",
		"scopes": []string{"bot:write"},
	})
	if minted.SetupCode.Code == "" || strings.Count(minted.SetupCode.Code, "-") != 2 {
		t.Fatalf("expected plaintext code in mint response, got %#v", minted.SetupCode)
	}
	if minted.SetupCode.ExpiresAt == "" {
		t.Fatalf("expected expiry in mint response, got %#v", minted.SetupCode)
	}

	// Bot tokens cannot mint setup codes.
	tokenResp := postJSONAsUser[struct {
		BotToken store.BotToken `json:"bot_token"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/bots/"+bot.Bot.ID+"/tokens", map[string]any{"name": "for authz check"})
	req, err := http.NewRequest(http.MethodPost, mintURL, strings.NewReader(`{"name":"gateway"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenResp.BotToken.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected bot token mint to be forbidden, got %s", resp.Status)
	}

	// Claim is unauthenticated and returns the minted token plus context.
	rawClaim := postJSON[map[string]json.RawMessage](t, server.URL+"/api/bot-setup-codes/claim", map[string]any{"code": minted.SetupCode.Code})
	if _, ok := rawClaim["bot_token"]; ok {
		t.Fatalf("claim response leaked internal bot token metadata: %#v", rawClaim)
	}
	var claim struct {
		Token string `json:"token"`
		Bot   struct {
			ID          string `json:"id"`
			Handle      string `json:"handle"`
			DisplayName string `json:"display_name"`
		} `json:"bot"`
		Workspace struct {
			ID      string `json:"id"`
			RouteID string `json:"route_id"`
			Slug    string `json:"slug"`
			Name    string `json:"name"`
		} `json:"workspace"`
		Defaults struct {
			DefaultTo string `json:"defaultTo"`
		} `json:"defaults"`
	}
	encodedClaim, err := json.Marshal(rawClaim)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encodedClaim, &claim); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(claim.Token, "ccb_") {
		t.Fatalf("expected plaintext token in claim response, got %#v", claim)
	}
	if claim.Bot.ID != bot.Bot.ID || claim.Workspace.ID != workspace.ID || claim.Workspace.RouteID == "" {
		t.Fatalf("unexpected claim context: %#v", claim)
	}
	if claim.Defaults.DefaultTo != "channel:general" {
		t.Fatalf("expected default channel suggestion, got %#v", claim.Defaults)
	}

	auditLog := getJSONAsUser[struct {
		AuditLogEntries []store.AuditLogEntry `json:"audit_log_entries"`
	}](t, owner.ID, server.URL+"/api/workspaces/"+workspace.ID+"/audit-log")
	var foundClaim bool
	for _, entry := range auditLog.AuditLogEntries {
		if entry.Action != "bot_setup_code.claimed" {
			continue
		}
		foundClaim = true
		if entry.ActorUserID != bot.Bot.ID {
			t.Fatalf("expected claimed token bot as audit actor, got %#v", entry)
		}
	}
	if !foundClaim {
		t.Fatalf("expected setup code claim audit entry, got %#v", auditLog.AuditLogEntries)
	}

	// Codes are single use, and unknown codes look identical.
	usedStatus, usedBody := setupCodeClaimError(t, server.URL, minted.SetupCode.Code)
	unknownStatus, unknownBody := setupCodeClaimError(t, server.URL, "AAAA-BBBB-CCCC")
	if usedStatus != http.StatusNotFound || unknownStatus != http.StatusNotFound || usedBody != unknownBody {
		t.Fatalf("expected uniform not-found responses, used=(%d %q) unknown=(%d %q)", usedStatus, usedBody, unknownStatus, unknownBody)
	}
}

func setupCodeClaimError(t *testing.T, serverURL, code string) (int, string) {
	t.Helper()
	resp, err := http.Post(
		serverURL+"/api/bot-setup-codes/claim",
		"application/json",
		strings.NewReader(`{"code":"`+code+`"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestHTTPBotSetupCodeClaimRateLimit(t *testing.T) {
	t.Parallel()
	server, _, _, _, _ := newSetupCodeTestServer(t)

	claimURL := server.URL + "/api/bot-setup-codes/claim"
	for i := 0; i < setupCodeClaimLimit; i++ {
		expectStatus(t, http.MethodPost, claimURL, strings.NewReader(`{"code":"AAAA-BBBB-CCCC"}`), http.StatusNotFound)
	}
	expectStatus(t, http.MethodPost, claimURL, strings.NewReader(`{"code":"AAAA-BBBB-CCCC"}`), http.StatusTooManyRequests)
}
