package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type currentUserResponse = struct {
	User currentUserPayload `json:"user"`
}

// The sidebar order roams with the account, so /api/me carries it beside the
// appearance snapshot and each section patches independently.
func TestSidebarPreferencesRoamWithTheAccount(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "sidebar-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := workspaces[0].ID
	first, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspaceID, Name: "aa-order", Kind: "public", UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspaceID, Name: "zz-order", Kind: "public", UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	server := newSidebarTestServer(t, st, dataDir)

	empty := getJSON[currentUserResponse](t, server.URL+"/api/me")
	if empty.User.SidebarPreferences != nil {
		t.Fatalf("a fresh account reported a sidebar order: %#v", empty.User.SidebarPreferences)
	}

	dark := patchJSON[currentUserResponse](t, server.URL+"/api/me", map[string]any{
		"appearance_preferences": map[string]any{"color_mode": "dark"},
	})
	if dark.User.AppearancePreferences == nil || dark.User.AppearancePreferences.ColorMode != "dark" {
		t.Fatalf("appearance patch did not apply: %#v", dark.User.AppearancePreferences)
	}

	// A sidebar patch leaves appearance alone, drops an id that is not a
	// channel of this workspace, and keeps the caller's order.
	saved := patchJSON[currentUserResponse](t, server.URL+"/api/me", map[string]any{
		"sidebar_preferences": map[string]any{
			"channel_order": map[string]any{workspaceID: []string{second.ID, "chn_stranger", first.ID}},
		},
	})
	assertSidebarOrder(t, saved.User.SidebarPreferences, workspaceID, second.ID, first.ID)
	if saved.User.AppearancePreferences == nil || saved.User.AppearancePreferences.ColorMode != "dark" {
		t.Fatalf("a sidebar patch changed appearance: %#v", saved.User.AppearancePreferences)
	}

	persisted := getJSON[currentUserResponse](t, server.URL+"/api/me")
	assertSidebarOrder(t, persisted.User.SidebarPreferences, workspaceID, second.ID, first.ID)

	// An appearance patch leaves the sidebar order alone.
	light := patchJSON[currentUserResponse](t, server.URL+"/api/me", map[string]any{
		"appearance_preferences": map[string]any{"color_mode": "light"},
	})
	if light.User.AppearancePreferences == nil || light.User.AppearancePreferences.ColorMode != "light" {
		t.Fatalf("appearance patch did not apply: %#v", light.User.AppearancePreferences)
	}
	assertSidebarOrder(t, light.User.SidebarPreferences, workspaceID, second.ID, first.ID)

	// A profile-only patch leaves both sections alone.
	renamed := patchJSON[currentUserResponse](t, server.URL+"/api/me", map[string]any{"display_name": "Renamed Owner"})
	if renamed.User.DisplayName != "Renamed Owner" {
		t.Fatalf("profile patch did not apply: %#v", renamed.User.User)
	}
	assertSidebarOrder(t, renamed.User.SidebarPreferences, workspaceID, second.ID, first.ID)
	if renamed.User.AppearancePreferences == nil || renamed.User.AppearancePreferences.ColorMode != "light" {
		t.Fatalf("profile patch changed appearance: %#v", renamed.User.AppearancePreferences)
	}

	// An empty list clears that workspace, and the cleared workspace keeps its
	// key. A client that cached the old order needs to see the clear; a
	// response that simply omitted the workspace would look identical to one
	// that never saved an order, and the cache would win on the next load.
	cleared := patchJSON[currentUserResponse](t, server.URL+"/api/me", map[string]any{
		"sidebar_preferences": map[string]any{"channel_order": map[string]any{workspaceID: []string{}}},
	})
	assertClearedSidebarOrder(t, cleared.User.SidebarPreferences, workspaceID)

	afterClear := getJSON[currentUserResponse](t, server.URL+"/api/me")
	assertClearedSidebarOrder(t, afterClear.User.SidebarPreferences, workspaceID)
	assertClearedSidebarOrderJSON(t, server.URL+"/api/me", workspaceID)
}

func TestSidebarPreferencesRejectNonMembers(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "sidebar-stranger-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	private, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Private", Slug: "private"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: private.ID, Name: "aa-private", Kind: "public", UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "sidebar-stranger@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, stranger.ID, "member"); err != nil {
		t.Fatal(err)
	}
	server := newSidebarTestServer(t, st, dataDir)

	body := `{"sidebar_preferences":{"channel_order":{"` + private.ID + `":["` + channel.ID + `"]}}}`
	expectStatusAsUser(t, stranger.ID, http.MethodPatch, server.URL+"/api/me", strings.NewReader(body), http.StatusForbidden)

	after := getJSONAsUser[currentUserResponse](t, stranger.ID, server.URL+"/api/me")
	if after.User.SidebarPreferences != nil {
		t.Fatalf("a rejected patch stored an order: %#v", after.User.SidebarPreferences)
	}
}

// assertClearedSidebarOrder pins the difference a client depends on: a cleared
// workspace keeps its key and holds an empty list, where a workspace that never
// saved an order is absent.
func assertClearedSidebarOrder(t *testing.T, preferences *store.SidebarPreferences, workspaceID string) {
	t.Helper()
	if preferences == nil {
		t.Fatalf("clearing the only workspace dropped the snapshot for %s", workspaceID)
	}
	got, ok := preferences.ChannelOrder[workspaceID]
	if !ok {
		t.Fatalf("cleared workspace %s lost its key: %#v", workspaceID, preferences.ChannelOrder)
	}
	if len(got) != 0 {
		t.Fatalf("cleared workspace %s kept an order: %#v", workspaceID, got)
	}
}

// assertClearedSidebarOrderJSON reads the wire bytes, because a nil slice would
// satisfy the typed assertion above and still reach the browser as null.
func assertClearedSidebarOrderJSON(t *testing.T, url, workspaceID string) {
	t.Helper()
	payload := getJSON[struct {
		User struct {
			SidebarPreferences struct {
				ChannelOrder map[string]json.RawMessage `json:"channel_order"`
			} `json:"sidebar_preferences"`
		} `json:"user"`
	}](t, url)
	raw, ok := payload.User.SidebarPreferences.ChannelOrder[workspaceID]
	if !ok {
		t.Fatalf("cleared workspace %s is missing from the response body", workspaceID)
	}
	if string(raw) != "[]" {
		t.Fatalf("cleared workspace %s serialized as %s, want []", workspaceID, raw)
	}
}

func newSidebarTestServer(t *testing.T, st *sqlitestore.Store, dataDir string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	return server
}

func assertSidebarOrder(t *testing.T, preferences *store.SidebarPreferences, workspaceID string, want ...string) {
	t.Helper()
	if preferences == nil {
		t.Fatalf("expected a sidebar snapshot for %s", workspaceID)
	}
	got := preferences.ChannelOrder[workspaceID]
	if len(got) != len(want) {
		t.Fatalf("unexpected order for %s: %#v", workspaceID, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected order for %s: %#v", workspaceID, got)
		}
	}
}
