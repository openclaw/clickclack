// Package workspacetest checks workspace settings against each SQL backend.
package workspacetest

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func ReservedSlugUpdates(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-reserved-slug@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("bootstrap workspaces: %#v, %v", workspaces, err)
	}
	workspace := workspaces[0]
	if workspace.Slug != "clickclack" || workspace.Role != store.WorkspaceRoleOwner {
		t.Fatalf("expected ownership of the provisioned workspace: %#v", workspace)
	}
	regular, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Regular", Slug: "regular"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		workspace store.Workspace
		slug      string
		wantSlug  string
		wantError string
	}{
		{name: "retain reserved slug", workspace: workspace, slug: "clickclack", wantSlug: "clickclack"},
		{name: "normalize retained slug", workspace: workspace, slug: "  CLICKCLACK  ", wantSlug: "clickclack"},
		{name: "reject reserved destination", workspace: workspace, slug: "  GUESTS  ", wantError: "workspace slug is reserved"},
		{name: "reject reserved takeover", workspace: regular, slug: "  CLICKCLACK  ", wantError: "workspace slug is reserved"},
		{name: "reject unclaimed reserved slug", workspace: regular, slug: "guests", wantError: "workspace slug is reserved"},
		{name: "reject empty slug", workspace: workspace, slug: "---", wantError: "workspace slug is required"},
		{name: "allow leaving reserved slug", workspace: workspace, slug: "renamed", wantSlug: "renamed"},
		{name: "reject reclaiming reserved slug", workspace: workspace, slug: "clickclack", wantError: "workspace slug is reserved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := st.GetWorkspace(ctx, tc.workspace.ID, owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Profile saves submit both name and slug, even when only the name changed.
			updated, event, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{
				WorkspaceID: before.ID, ActorUserID: owner.ID, Name: &tc.name, Slug: &tc.slug,
			})
			if tc.wantError != "" {
				if err == nil || err.Error() != tc.wantError {
					t.Fatalf("expected %q, got %v", tc.wantError, err)
				}
				if event.ID != "" {
					t.Fatalf("rejected update returned an event: %#v", event)
				}
			} else {
				if err != nil {
					t.Fatalf("profile update: %v", err)
				}
				if updated.Name != tc.name || updated.Slug != tc.wantSlug || event.Type != "workspace.updated" {
					t.Fatalf("unexpected profile update: %#v, %#v", updated, event)
				}
			}
			persisted, err := st.GetWorkspace(ctx, before.ID, owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := updated
			if tc.wantError != "" {
				want = before
			}
			if persisted != want {
				t.Fatalf("persisted workspace = %#v, want %#v", persisted, want)
			}
		})
	}
}
