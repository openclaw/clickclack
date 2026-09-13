package sqlite

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSidebarChannelOrderLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Sidebar User",
		Email:       "sidebar-sqlite@example.com",
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

	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Sidebar", Slug: "sidebar"}, user.ID)
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
	other, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Sidebar", Slug: "other-sidebar"}, user.ID)
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
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "sidebar-owner-sqlite@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "sidebar-stranger-sqlite@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Members Only", Slug: "members-only"}, owner.ID)
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
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Cap User", Email: "sidebar-cap-sqlite@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Cap", Slug: "cap"}, user.ID)
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

func TestSidebarChannelOrderCascadesWithMembership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Cascade User", Email: "sidebar-cascade-sqlite@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Cascade", Slug: "cascade"}, user.ID)
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
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, user.ID); err != nil {
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
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Both User", Email: "sidebar-both-sqlite@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Both", Slug: "both"}, user.ID)
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
