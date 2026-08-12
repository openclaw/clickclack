package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestAddWorkspaceMemberByActorPostgres(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "pg-member-add-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Postgres Member Add"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "pg-member-add-actor@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	target, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Target", Email: "pg-member-add-target@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddWorkspaceMemberByActor(ctx, store.AddWorkspaceMemberInput{
		WorkspaceID: workspace.ID,
		UserID:      target.ID,
		ActorUserID: member.ID,
		Role:        store.WorkspaceRoleMember,
	}); !errors.Is(err, store.ErrNotWorkspaceManager) {
		t.Fatalf("expected member actor rejection, got %v", err)
	}
	first, err := st.AddWorkspaceMemberByActor(ctx, store.AddWorkspaceMemberInput{
		WorkspaceID: workspace.ID,
		UserID:      target.ID,
		ActorUserID: owner.ID,
		Role:        store.WorkspaceRoleModerator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Added || first.Role != store.WorkspaceRoleModerator {
		t.Fatalf("unexpected insert result: %#v", first)
	}
	replay, err := st.AddWorkspaceMemberByActor(ctx, store.AddWorkspaceMemberInput{
		WorkspaceID: workspace.ID,
		UserID:      target.ID,
		ActorUserID: owner.ID,
		Role:        store.WorkspaceRoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Added || replay.Role != store.WorkspaceRoleModerator {
		t.Fatalf("idempotent add changed or misreported the role: %#v", replay)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspace.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	readded, err := st.AddWorkspaceMemberByActor(ctx, store.AddWorkspaceMemberInput{
		WorkspaceID: workspace.ID,
		UserID:      target.ID,
		ActorUserID: owner.ID,
		Role:        store.WorkspaceRoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !readded.Added || readded.Role != store.WorkspaceRoleMember {
		t.Fatalf("re-add restored a stale privileged role: %#v", readded)
	}
}
