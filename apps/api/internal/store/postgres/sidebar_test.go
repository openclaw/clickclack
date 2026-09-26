package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSidebarChannelOrderLifecycle(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Sidebar User",
		Email:       "sidebar-postgres-" + suffix + "@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	preferences, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences != nil {
		t.Fatalf("expected missing preferences, got %#v", preferences)
	}

	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Sidebar", Slug: "sidebar-" + suffix}, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "aa", Kind: "public", UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "bb", Kind: "public", UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}

	account, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:             user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.SidebarPreferences != nil {
		t.Fatalf("empty patch created a row: %#v", account.SidebarPreferences)
	}

	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {second.ID, first.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, second.ID, first.ID)

	roundTrip, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertChannelOrder(t, roundTrip, workspace.ID, second.ID, first.ID)

	// An id that is not a channel of this workspace is dropped rather than
	// rejected, so a channel deleted since the client rendered cannot wedge a
	// save. Duplicates keep their first position.
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {first.ID, "chn_gone", second.ID, first.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, first.ID, second.ID)

	// A second workspace is stored independently.
	other, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Sidebar", Slug: "other-sidebar-" + suffix}, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: other.ID, Name: "cc", Kind: "public", UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{other.ID: {otherChannel.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, first.ID, second.ID)
	assertChannelOrder(t, account.SidebarPreferences, other.ID, otherChannel.ID)

	// A channel of the other workspace never enters this workspace's order.
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {otherChannel.ID, second.ID, first.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, second.ID, first.ID)

	// An empty list clears that workspace and leaves the rest alone. The
	// cleared workspace keeps its key, holding an empty list, so a client can
	// tell a clear from a workspace that never saved an order and drop its own
	// cached copy instead of restoring it.
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.SidebarPreferences == nil {
		t.Fatal("clearing one workspace dropped the whole snapshot")
	}
	assertClearedChannelOrder(t, account.SidebarPreferences, workspace.ID)
	assertChannelOrder(t, account.SidebarPreferences, other.ID, otherChannel.ID)

	cleared, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertClearedChannelOrder(t, cleared, workspace.ID)
	assertChannelOrder(t, cleared, other.ID, otherChannel.ID)
}

func TestSidebarChannelOrderRequiresMembership(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "sidebar-owner-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "sidebar-stranger-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Members Only", Slug: "members-only-" + suffix}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "aa", Kind: "public", UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: stranger.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {channel.ID}},
		},
	})
	if !errors.Is(err, store.ErrNotWorkspaceMember) {
		t.Fatalf("expected a membership error, got %v", err)
	}
	preferences, err := st.GetSidebarPreferences(ctx, stranger.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences != nil {
		t.Fatalf("a non-member wrote an order: %#v", preferences)
	}
}

func TestSidebarChannelOrderRejectsOversizedPatch(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Cap User", Email: "sidebar-cap-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Cap", Slug: "cap-" + suffix}, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	overLimit := make([]string, 0, store.MaxSidebarChannelOrderIDs+1)
	for i := 0; i <= store.MaxSidebarChannelOrderIDs; i++ {
		overLimit = append(overLimit, "chn_"+strconv.Itoa(i))
	}
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: overLimit},
		},
	}); err == nil {
		t.Fatal("expected the per-workspace cap to reject the patch")
	}
}

func TestSidebarPreferencesReadReturnsEveryWorkspace(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Wide User", Email: "sidebar-wide-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// The workspace cap bounds one patch, not the snapshot: a member of more
	// workspaces than one patch may list reads back every saved order.
	want := make(map[string]string, store.MaxSidebarChannelOrderWorkspaces+1)
	for i := 0; i <= store.MaxSidebarChannelOrderWorkspaces; i++ {
		workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Wide", Slug: "wide-" + suffix + "-" + strconv.Itoa(i)}, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "aa", Kind: "public", UserID: user.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
			UserID: user.ID,
			SidebarPreferences: &store.SidebarPreferencesPatch{
				ChannelOrder: map[string][]string{workspace.ID: {channel.ID}},
			},
		}); err != nil {
			t.Fatal(err)
		}
		want[workspace.ID] = channel.ID
	}
	preferences, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences == nil || len(preferences.ChannelOrder) != store.MaxSidebarChannelOrderWorkspaces+1 {
		t.Fatalf("expected %d saved orders, got %#v", store.MaxSidebarChannelOrderWorkspaces+1, preferences)
	}
	for workspaceID, channelID := range want {
		assertChannelOrder(t, preferences, workspaceID, channelID)
	}
}

func TestSidebarChannelOrderCascadesWithMembership(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Cascade User", Email: "sidebar-cascade-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Cascade", Slug: "cascade-" + suffix}, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "aa", Kind: "public", UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {channel.ID}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspace.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	preferences, err := st.GetSidebarPreferences(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preferences != nil {
		t.Fatalf("membership removal did not cascade the saved order: %#v", preferences)
	}
}

func TestSidebarChannelOrderIndependentOfAppearance(t *testing.T) {
	ctx, st, suffix := newSidebarTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Both User", Email: "sidebar-both-postgres-" + suffix + "@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Both", Slug: "both-" + suffix}, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "aa", Kind: "public", UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}

	dark := "dark"
	if _, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &dark},
	}); err != nil {
		t.Fatal(err)
	}
	account, err := st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: user.ID,
		SidebarPreferences: &store.SidebarPreferencesPatch{
			ChannelOrder: map[string][]string{workspace.ID: {channel.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.AppearancePreferences == nil || account.AppearancePreferences.ColorMode != "dark" {
		t.Fatalf("a sidebar patch changed appearance: %#v", account.AppearancePreferences)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, channel.ID)

	light := "light"
	account, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID:                user.ID,
		AppearancePreferences: &store.AppearancePreferencesPatch{ColorMode: &light},
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.AppearancePreferences == nil || account.AppearancePreferences.ColorMode != "light" {
		t.Fatalf("unexpected appearance: %#v", account.AppearancePreferences)
	}
	assertChannelOrder(t, account.SidebarPreferences, workspace.ID, channel.ID)
}

func newSidebarTestStore(t *testing.T) (context.Context, *Store, string) {
	t.Helper()
	dsn := os.Getenv("CLICKCLACK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set CLICKCLACK_POSTGRES_TEST_DSN to run Postgres integration smoke")
	}
	ctx := context.Background()
	st, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, st, time.Now().UTC().Format("20060102150405.000000000")
}

// assertClearedChannelOrder pins the difference between a workspace whose order
// was cleared and one that never saved one: the cleared workspace keeps its key
// and holds an empty list.
func assertClearedChannelOrder(t *testing.T, preferences *store.SidebarPreferences, workspaceID string) {
	t.Helper()
	if preferences == nil {
		t.Fatalf("expected a sidebar snapshot for %s", workspaceID)
	}
	got, ok := preferences.ChannelOrder[workspaceID]
	if !ok {
		t.Fatalf("cleared workspace %s lost its key: %#v", workspaceID, preferences.ChannelOrder)
	}
	if got == nil {
		t.Fatalf("cleared workspace %s reported a null order", workspaceID)
	}
	if len(got) != 0 {
		t.Fatalf("cleared workspace %s kept an order: %#v", workspaceID, got)
	}
}

func assertChannelOrder(t *testing.T, preferences *store.SidebarPreferences, workspaceID string, want ...string) {
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
