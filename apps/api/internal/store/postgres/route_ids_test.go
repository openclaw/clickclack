package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestMessageRouteIDCreationRespectsChannelAndDirectBoundaries(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Route Owner",
		Email:       "postgres-route-owner@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Route Member",
		Email:       "postgres-route-member@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Route Boundaries"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Name:        "route-boundaries",
		Kind:        "public",
	})
	if err != nil {
		t.Fatal(err)
	}

	channelRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "channel root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(channelRoot.RouteID, "M") {
		t.Fatalf("channel root should receive an eager M route_id: %#v", channelRoot)
	}

	direct, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	directRoot, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID,
		AuthorID:       owner.ID,
		Body:           "direct root",
	})
	if err != nil {
		t.Fatal(err)
	}
	if directRoot.RouteID != "" {
		t.Fatalf("direct root should preserve lazy M route allocation: %#v", directRoot)
	}

	directRoot, err = st.EnsureThreadRouteID(ctx, owner.ID, directRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(directRoot.RouteID, "M") {
		t.Fatalf("direct root should receive an M route_id only through the compatible thread path: %#v", directRoot)
	}
}
