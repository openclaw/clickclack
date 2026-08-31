package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/config"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestDispatchArgsDefaultsNoArgumentInvocationToServe(t *testing.T) {
	cmd, args, clientArgs := dispatchArgs([]string{"clickclack"})
	if cmd != "serve" || len(args) != 0 || len(clientArgs) != 0 {
		t.Fatalf("unexpected dispatch: cmd=%q args=%v clientArgs=%v", cmd, args, clientArgs)
	}
}

func TestExportDataPreservesExistingOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "empty.db")
	outPath := filepath.Join(dir, "export.json")
	if err := os.WriteFile(outPath, []byte("previous export"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := exportData([]string{"--db", "sqlite://" + dbPath, "--out", outPath})
	if err == nil {
		t.Fatal("expected export failure for database without schema")
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "previous export" {
		t.Fatalf("existing export was overwritten: %q", body)
	}
}

func TestCommandDBDefaultsUseEnvironment(t *testing.T) {
	t.Setenv("CLICKCLACK_DATA", "/tmp/clickclack-env-data")
	t.Setenv("CLICKCLACK_DB", "postgres://example.invalid/clickclack")
	t.Setenv("CLICKCLACK_UPLOADS", "r2://bucket/uploads")
	if got := defaultData(); got != "/tmp/clickclack-env-data" {
		t.Fatalf("defaultData = %q", got)
	}
	if got := defaultDB(); got != "postgres://example.invalid/clickclack" {
		t.Fatalf("defaultDB = %q", got)
	}
	if got := defaultUploads(); got != "r2://bucket/uploads" {
		t.Fatalf("defaultUploads = %q", got)
	}
}

func TestApplyFlagOverridesParsesEmbedFrameAncestors(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("embed-frame-ancestors", "", "")
	if err := flags.Parse([]string{"--embed-frame-ancestors", "https://control.example.com,https://dock.example.com"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	applyFlagOverrides(flags, &cfg)
	if len(cfg.EmbedFrameAncestors) != 2 || cfg.EmbedFrameAncestors[1] != "https://dock.example.com" {
		t.Fatalf("unexpected embed frame ancestors: %#v", cfg.EmbedFrameAncestors)
	}
}

func TestApplyFlagOverridesSetsAccessConfig(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("access-team-domain", "", "")
	flags.String("access-aud", "", "")
	if err := flags.Parse([]string{"--access-team-domain", "https://openclaw.cloudflareaccess.com", "--access-aud", "test-aud"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	applyFlagOverrides(flags, &cfg)
	if cfg.AccessTeamDomain != "https://openclaw.cloudflareaccess.com" || cfg.AccessAUD != "test-aud" {
		t.Fatalf("unexpected Access flag config: %#v", cfg)
	}
}

func TestFakeCoSeedRequiresExplicitEnvironment(t *testing.T) {
	t.Setenv("CLICKCLACK_ENVIRONMENT", "")
	err := admin([]string{"fakeco", "seed", "--data", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), `must equal "fakeco"`) {
		t.Fatalf("expected FakeCo environment refusal, got %v", err)
	}
}

func TestAdminBotCreateValidatesActorBeforeOpeningDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")
	err := admin([]string{
		"bot", "create",
		"--db", "sqlite://" + dbPath,
		"--workspace", "wsp_missing",
		"--name", "Missing Actor",
	})
	if err == nil || err.Error() != "--created-by is required" {
		t.Fatalf("expected missing actor error, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("validation opened the database before rejecting the command: %v", statErr)
	}
}

func TestAdminBotCreateUsesExplicitAuthorizedActor(t *testing.T) {
	ctx := context.Background()
	dbURL := "sqlite://" + filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "cli-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "CLI Bots", Slug: "cli-bots"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "cli-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	if err := admin([]string{
		"bot", "create",
		"--db", dbURL,
		"--workspace", workspace.ID,
		"--created-by", owner.ID,
		"--name", "CLI Service",
		"--handle", "cli-service",
	}); err != nil {
		t.Fatalf("create service bot: %v", err)
	}
	if err := admin([]string{
		"bot", "create",
		"--db", dbURL,
		"--workspace", workspace.ID,
		"--owner", member.ID,
		"--created-by", member.ID,
		"--name", "CLI Personal",
		"--handle", "cli-personal",
	}); err != nil {
		t.Fatalf("create user-owned bot: %v", err)
	}
	err = admin([]string{
		"bot", "create",
		"--db", dbURL,
		"--workspace", workspace.ID,
		"--owner", member.ID,
		"--created-by", owner.ID,
		"--name", "Wrong Actor",
	})
	if !errors.Is(err, store.ErrBotOwnerCreateRequired) {
		t.Fatalf("expected mismatched owner rejection, got %v", err)
	}

	st, err = sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bots, err := st.ListBots(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("expected two bots, got %#v", bots)
	}
	ownersByHandle := make(map[string]string, len(bots))
	for _, bot := range bots {
		ownersByHandle[bot.Bot.Handle] = bot.Bot.OwnerUserID
	}
	if ownersByHandle["cli-service"] != "" || ownersByHandle["cli-personal"] != member.ID {
		t.Fatalf("unexpected bot ownership: %#v", ownersByHandle)
	}
}

func TestAdminMemberAddAddsExistingUserToSecondWorkspace(t *testing.T) {
	fixture := setupAdminMemberTest(t)

	output := captureStdout(t, func() error {
		return admin([]string{
			"member", "add",
			"--db", fixture.dbURL,
			"--workspace", fixture.second.ID,
			"--created-by", fixture.owner.ID,
			"--email", " Existing@Example.COM ",
		})
	})
	wantOutput := fmt.Sprintf("workspace=%s user=%s role=member status=added\n", fixture.second.ID, fixture.user.ID)
	if output != wantOutput {
		t.Fatalf("unexpected output: got %q want %q", output, wantOutput)
	}

	ctx := context.Background()
	st, err := sqlitestore.Open(fixture.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	workspaces, err := st.ListWorkspaces(ctx, fixture.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected two workspace memberships, got %#v", workspaces)
	}
	for _, workspace := range workspaces {
		if workspace.ID == fixture.second.ID && workspace.Role == store.WorkspaceRoleMember {
			return
		}
	}
	t.Fatalf("second workspace membership not found: %#v", workspaces)
}

func TestAdminMemberAddRejectsUnknownEmail(t *testing.T) {
	fixture := setupAdminMemberTest(t)
	err := admin([]string{
		"member", "add",
		"--db", fixture.dbURL,
		"--workspace", fixture.second.ID,
		"--created-by", fixture.owner.ID,
		"--email", " Missing@Example.COM ",
	})
	if err == nil || !strings.Contains(err.Error(), `no user found for email "Missing@Example.COM"`) {
		t.Fatalf("expected unknown email error, got %v", err)
	}
	if !strings.Contains(err.Error(), "clickclack admin user create --workspace "+fixture.second.ID) {
		t.Fatalf("expected actionable user create guidance, got %v", err)
	}
}

func TestAdminMemberAddValidatesRequiredFlagsBeforeOpeningDatabase(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "workspace", args: []string{"--created-by", "usr_owner", "--email", "existing@example.com"}, wantErr: "--workspace is required"},
		{name: "actor", args: []string{"--workspace", "wsp_missing", "--email", "existing@example.com"}, wantErr: "--created-by is required"},
		{name: "selector", args: []string{"--workspace", "wsp_missing", "--created-by", "usr_owner"}, wantErr: "exactly one of --email or --user is required"},
		{name: "selectors", args: []string{"--workspace", "wsp_missing", "--created-by", "usr_owner", "--email", "existing@example.com", "--user", "usr_existing"}, wantErr: "exactly one of --email or --user is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "missing.db")
			args := append([]string{"member", "add", "--db", "sqlite://" + dbPath}, test.args...)
			err := admin(args)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("expected %q, got %v", test.wantErr, err)
			}
			if _, statErr := os.Stat(dbPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("validation opened the database before rejecting the command: %v", statErr)
			}
		})
	}
}

func TestAdminMemberAddRejectsInvalidRole(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")
	err := admin([]string{
		"member", "add",
		"--db", "sqlite://" + dbPath,
		"--workspace", "wsp_missing",
		"--created-by", "usr_owner",
		"--email", "existing@example.com",
		"--role", store.WorkspaceRoleGuest,
	})
	if err == nil || !strings.Contains(err.Error(), "--role must be one of member or moderator") {
		t.Fatalf("expected invalid role error, got %v", err)
	}
	if _, statErr := os.Stat(dbPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("validation opened the database before rejecting the command: %v", statErr)
	}
}

func TestAdminMemberAddIsIdempotentForExistingMember(t *testing.T) {
	fixture := setupAdminMemberTest(t)

	output := captureStdout(t, func() error {
		return admin([]string{
			"member", "add",
			"--db", fixture.dbURL,
			"--workspace", fixture.first.ID,
			"--created-by", fixture.owner.ID,
			"--email", "existing@example.com",
			"--role", store.WorkspaceRoleModerator,
		})
	})
	wantOutput := fmt.Sprintf("workspace=%s user=%s role=member status=already_member\n", fixture.first.ID, fixture.user.ID)
	if output != wantOutput {
		t.Fatalf("unexpected output: got %q want %q", output, wantOutput)
	}

	ctx := context.Background()
	st, err := sqlitestore.Open(fixture.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	workspaces, err := st.ListWorkspaces(ctx, fixture.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != fixture.first.ID || workspaces[0].Role != store.WorkspaceRoleMember {
		t.Fatalf("existing membership changed: %#v", workspaces)
	}
}

// Regression: admin user create stores the address exactly as typed, so a
// mixed-case identity must still resolve from a differently-cased lookup.
func TestAdminMemberAddResolvesMixedCaseIdentityEmail(t *testing.T) {
	ctx := context.Background()
	dbURL := "sqlite://" + filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Mixed", Slug: "mixed"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Mixed", Email: "Existing@Example.COM"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return admin([]string{
			"member", "add",
			"--db", dbURL,
			"--workspace", workspace.ID,
			"--created-by", owner.ID,
			"--email", "existing@example.com",
		})
	})
	wantOutput := fmt.Sprintf("workspace=%s user=%s role=member status=added\n", workspace.ID, mixed.ID)
	if output != wantOutput {
		t.Fatalf("unexpected output: got %q want %q", output, wantOutput)
	}
}

func TestAdminMemberAddRejectsAmbiguousEmailAndAcceptsUserID(t *testing.T) {
	fixture := setupAdminMemberTest(t)
	ctx := context.Background()
	st, err := sqlitestore.Open(fixture.dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Duplicate", Email: "EXISTING@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	err = admin([]string{
		"member", "add",
		"--db", fixture.dbURL,
		"--workspace", fixture.second.ID,
		"--created-by", fixture.owner.ID,
		"--email", "existing@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "multiple users found") || !strings.Contains(err.Error(), "--user USER_ID") {
		t.Fatalf("expected actionable ambiguous-email error, got %v", err)
	}

	output := captureStdout(t, func() error {
		return admin([]string{
			"member", "add",
			"--db", fixture.dbURL,
			"--workspace", fixture.second.ID,
			"--created-by", fixture.owner.ID,
			"--user", fixture.user.ID,
		})
	})
	wantOutput := fmt.Sprintf("workspace=%s user=%s role=member status=added\n", fixture.second.ID, fixture.user.ID)
	if output != wantOutput {
		t.Fatalf("unexpected ID-selected output: got %q want %q", output, wantOutput)
	}
}

func TestAdminMemberAddEnforcesManagerRoleInStore(t *testing.T) {
	fixture := setupAdminMemberTest(t)
	err := admin([]string{
		"member", "add",
		"--db", fixture.dbURL,
		"--workspace", fixture.second.ID,
		"--created-by", fixture.user.ID,
		"--user", fixture.user.ID,
	})
	if !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected non-manager rejection, got %v", err)
	}
}

func TestAdminMemberAddRejectsUnknownUserID(t *testing.T) {
	fixture := setupAdminMemberTest(t)
	err := admin([]string{
		"member", "add",
		"--db", fixture.dbURL,
		"--workspace", fixture.second.ID,
		"--created-by", fixture.owner.ID,
		"--user", "usr_missing",
	})
	if err == nil || err.Error() != `no user found for id "usr_missing"` {
		t.Fatalf("expected unknown user ID error, got %v", err)
	}
}

type adminMemberFixture struct {
	dbURL         string
	owner, user   store.User
	first, second store.Workspace
}

func setupAdminMemberTest(t *testing.T) adminMemberFixture {
	t.Helper()
	ctx := context.Background()
	dbURL := "sqlite://" + filepath.Join(t.TempDir(), "clickclack.db")
	st, err := sqlitestore.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "First", Slug: "first"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Second", Slug: "second"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Existing", Email: "existing@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, first.ID, user.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return adminMemberFixture{dbURL: dbURL, owner: owner, user: user, first: first, second: second}
}

func TestOpenUploadStorageValidation(t *testing.T) {
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: "r2://bucket/prod"}); err == nil {
		t.Fatal("expected missing r2 credentials error")
	}
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: "file://" + t.TempDir()}); err != nil {
		t.Fatalf("file upload storage: %v", err)
	}
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: t.TempDir()}); err != nil {
		t.Fatalf("plain upload storage path: %v", err)
	}
}

func TestMessagesListOmitsAfterSeqUntilExplicitlySet(t *testing.T) {
	var messagePaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{{ID: "wsp_1", Slug: "one", Name: "One"}}})
		case "/api/workspaces/wsp_1/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{{ID: "chn_1", WorkspaceID: "wsp_1", Name: "general"}}})
		case "/api/channels/chn_1/messages":
			messagePaths = append(messagePaths, r.URL.RawQuery)
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": []store.Message{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: "wsp_1", Channel: "chn_1", Plain: true}, http: server.Client()}
	if err := c.messagesList([]string{"--limit", "2"}); err != nil {
		t.Fatal(err)
	}
	if len(messagePaths) != 1 {
		t.Fatalf("expected one messages request, got %d", len(messagePaths))
	}
	if strings.Contains(messagePaths[0], "after_seq=") {
		t.Fatalf("unexpected after_seq in default query: %q", messagePaths[0])
	}
	if err := c.messagesList([]string{"--limit", "2", "--after-seq", "4"}); err != nil {
		t.Fatal(err)
	}
	if len(messagePaths) != 2 || !strings.Contains(messagePaths[1], "after_seq=4") {
		t.Fatalf("expected explicit after_seq query, got %v", messagePaths)
	}
}

func TestStatusSelectsMatchingWorkspaceAndChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user": store.User{ID: "usr_1", DisplayName: "User"}})
		case "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{
				{ID: "wsp_1", Slug: "one", Name: "One"}, {ID: "wsp_2", Slug: "two", Name: "Two"},
			}})
		case "/api/workspaces/wsp_1/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{
				{ID: "chn_misc", WorkspaceID: "wsp_1", Name: "misc"}, {ID: "chn_1", WorkspaceID: "wsp_1", Name: "general"},
			}})
		case "/api/workspaces/wsp_2/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{{ID: "chn_2", WorkspaceID: "wsp_2", Name: "proof"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	for _, tt := range []struct {
		name, workspace, channel, wantWorkspace, wantChannel, wantError string
	}{
		{name: "general default", wantWorkspace: "wsp_1", wantChannel: "chn_1"},
		{name: "first channel default", workspace: "TWO", wantWorkspace: "wsp_2", wantChannel: "chn_2"},
		{name: "channel ID across workspaces", channel: "chn_2", wantWorkspace: "wsp_2", wantChannel: "chn_2"},
		{name: "channel name", workspace: "two", channel: "proof", wantWorkspace: "wsp_2", wantChannel: "chn_2"},
		{name: "hash name", workspace: "wsp_2", channel: "#proof", wantWorkspace: "wsp_2", wantChannel: "chn_2"},
		{name: "workspace constrains ID", workspace: "wsp_1", channel: "chn_2", wantError: `channel "chn_2" not found`},
		{name: "missing workspace", workspace: "missing", wantError: `workspace "missing" not found`},
		{name: "missing channel", workspace: "wsp_1", channel: "missing", wantError: `channel "missing" not found`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: tt.workspace, Channel: tt.channel, JSON: true}, http: server.Client()}
			var err error
			output := captureStdout(t, func() error { err = c.status(nil); return nil })
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || output != "" {
					t.Fatalf("expected %q without stdout, got %v, %q", tt.wantError, err, output)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Workspace store.Workspace `json:"workspace"`
				Channel   store.Channel   `json:"channel"`
			}
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if result.Workspace.ID != tt.wantWorkspace || result.Channel.ID != tt.wantChannel || result.Channel.WorkspaceID != result.Workspace.ID {
				t.Fatalf("mismatched selection: workspace=%s channel=%s channel workspace=%s", result.Workspace.ID, result.Channel.ID, result.Channel.WorkspaceID)
			}
		})
	}
}

func TestStatusDistinguishesEmptyListsFromDiscoveryErrors(t *testing.T) {
	for _, owner := range []string{"workspaces", "channels"} {
		for _, tt := range []struct {
			name, body, wantError string
			code                  int
		}{
			{name: "empty", code: http.StatusOK, body: fmt.Sprintf(`{"%s":[]}`, owner)},
			{name: "forbidden", code: http.StatusForbidden, body: `{"error":"denied"}`, wantError: "403 Forbidden: denied"},
			{name: "unavailable", code: http.StatusServiceUnavailable, body: `{"error":"unavailable"}`, wantError: "503 Service Unavailable: unavailable"},
			{name: "malformed", code: http.StatusOK, body: "not-json", wantError: "invalid character"},
		} {
			t.Run(owner+"/"+tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/api/"+owner || owner == "channels" && r.URL.Path == "/api/workspaces/wsp_1/channels" {
						w.WriteHeader(tt.code)
						_, _ = io.WriteString(w, tt.body)
						return
					}
					switch r.URL.Path {
					case "/api/me":
						_, _ = io.WriteString(w, `{"user":{"id":"usr_1"}}`)
					case "/api/workspaces":
						_, _ = io.WriteString(w, `{"workspaces":[{"id":"wsp_1"}]}`)
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(server.Close)
				c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", JSON: true}, http: server.Client()}
				var err error
				output := captureStdout(t, func() error { err = c.status(nil); return nil })
				if tt.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantError) || output != "" {
						t.Fatalf("expected %q without stdout, got %v, %q", tt.wantError, err, output)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Workspace store.Workspace `json:"workspace"`
					Channel   store.Channel   `json:"channel"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					t.Fatal(err)
				}
				wantWorkspace := ""
				if owner == "channels" {
					wantWorkspace = "wsp_1"
				}
				if result.Workspace.ID != wantWorkspace || result.Channel.ID != "" {
					t.Fatalf("unexpected empty selection: %#v", result)
				}
				c.opts.Channel = "general"
				output = captureStdout(t, func() error { err = c.status(nil); return nil })
				if err == nil || output != "" {
					t.Fatalf("explicit channel must fail on an empty list without stdout, got %v, %q", err, output)
				}
			})
		}
	}
}
