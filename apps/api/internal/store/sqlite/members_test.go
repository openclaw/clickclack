package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestListWorkspaceMemberPagePaginatesFiltersAndSearches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "members-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "members-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	alpha, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Alpha", Email: "members-alpha@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, alpha.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	beta, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "beta", Email: "members-beta@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, beta.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	percent, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "100% Real", Email: "members-percent@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, percent.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "members-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}

	first, err := st.ListWorkspaceMemberPage(ctx, workspace.ID, alpha.ID, store.WorkspaceMemberPageRequest{Limit: 2, Role: store.WorkspaceRoleMember})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("expected more member results, got %#v", first)
	}
	if first.TotalCount == nil || *first.TotalCount != 3 {
		t.Fatalf("expected first-page member count 3, got %#v", first.TotalCount)
	}
	if got := memberNames(first.Members); len(got) != 2 || got[0] != "100% Real" || got[1] != "Alpha" {
		t.Fatalf("unexpected first page order: %#v", got)
	}
	if first.Members[0].WorkspaceID != workspace.ID || first.Members[0].JoinedAt == "" {
		t.Fatalf("expected workspace membership metadata, got %#v", first.Members[0])
	}

	second, err := st.ListWorkspaceMemberPage(ctx, workspace.ID, alpha.ID, store.WorkspaceMemberPageRequest{Limit: 2, Role: store.WorkspaceRoleMember, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Fatalf("expected final page, got %#v", second)
	}
	if second.TotalCount != nil {
		t.Fatalf("expected cursor page to omit total count, got %#v", second.TotalCount)
	}
	if got := memberNames(second.Members); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("unexpected second page order: %#v", got)
	}

	search, err := st.ListWorkspaceMemberPage(ctx, workspace.ID, alpha.ID, store.WorkspaceMemberPageRequest{Query: "ALP"})
	if err != nil {
		t.Fatal(err)
	}
	if search.TotalCount == nil || *search.TotalCount != 1 {
		t.Fatalf("expected search count 1, got %#v", search.TotalCount)
	}
	if got := memberNames(search.Members); len(got) != 1 || got[0] != "Alpha" {
		t.Fatalf("unexpected search results: %#v", got)
	}
	literalPercent, err := st.ListWorkspaceMemberPage(ctx, workspace.ID, alpha.ID, store.WorkspaceMemberPageRequest{Query: "%"})
	if err != nil {
		t.Fatal(err)
	}
	if literalPercent.TotalCount == nil || *literalPercent.TotalCount != 1 {
		t.Fatalf("expected literal percent count 1, got %#v", literalPercent.TotalCount)
	}
	if got := memberNames(literalPercent.Members); len(got) != 1 || got[0] != "100% Real" {
		t.Fatalf("expected literal percent search, got %#v", got)
	}

	_, err = st.ListWorkspaceMemberPage(ctx, workspace.ID, alpha.ID, store.WorkspaceMemberPageRequest{Limit: 2, Role: store.WorkspaceRoleGuest, Cursor: first.NextCursor})
	if !errors.Is(err, store.ErrInvalidWorkspaceMemberPage) {
		t.Fatalf("expected cursor filter mismatch rejection, got %v", err)
	}
}

func memberNames(members []store.WorkspaceMember) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.User.DisplayName)
	}
	return names
}
