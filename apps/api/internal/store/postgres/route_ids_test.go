package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestMigrateBackfillsLegacyChannelRootRouteIDs(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	applyPostgresMigrationsBefore(t, ctx, st, "0002_default_workspace_owner.sql")

	const (
		ownerID       = "usr_legacy_route_owner"
		memberID      = "usr_legacy_route_member"
		workspaceID   = "wsp_legacy_route"
		channelID     = "chn_legacy_route"
		directID      = "dct_legacy_route"
		channelRootID = "msg_legacy_channel_root"
		directRootID  = "msg_legacy_direct_root"
		replyID       = "msg_legacy_channel_reply"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, display_name, avatar_url, created_at) VALUES ($1, 'Owner', '', $2)`, []any{ownerID, now()}},
		{`INSERT INTO users (id, display_name, avatar_url, created_at) VALUES ($1, 'Member', '', $2)`, []any{memberID, now()}},
		{`INSERT INTO workspaces (id, name, slug, created_at) VALUES ($1, 'Legacy Routes', 'legacy-routes', $2)`, []any{workspaceID, now()}},
		{`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES ($1, $2, 'owner', $3), ($1, $4, 'member', $3)`, []any{workspaceID, ownerID, now(), memberID}},
		{`INSERT INTO channels (id, workspace_id, name, kind, created_at) VALUES ($1, $2, 'general', 'public', $3)`, []any{channelID, workspaceID, now()}},
		{`INSERT INTO direct_conversations (id, workspace_id, created_at) VALUES ($1, $2, $3)`, []any{directID, workspaceID, now()}},
		{`INSERT INTO messages (id, workspace_id, channel_id, author_id, thread_root_id, channel_seq, body, body_format, created_at) VALUES ($1, $2, $3, $4, $1, 1, 'channel root', 'markdown', $5)`, []any{channelRootID, workspaceID, channelID, ownerID, now()}},
		{`INSERT INTO messages (id, workspace_id, direct_conversation_id, author_id, thread_root_id, channel_seq, body, body_format, created_at) VALUES ($1, $2, $3, $4, $1, 1, 'direct root', 'markdown', $5)`, []any{directRootID, workspaceID, directID, ownerID, now()}},
		{`INSERT INTO messages (id, workspace_id, channel_id, author_id, parent_message_id, thread_root_id, thread_seq, body, body_format, created_at) VALUES ($1, $2, $3, $4, $5, $5, 1, 'reply', 'markdown', $6)`, []any{replyID, workspaceID, channelID, memberID, channelRootID, now()}},
	} {
		if _, err := st.db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var workspaceRouteID, channelRouteID, directRouteID, channelRootRouteID, directRootRouteID, replyRouteID sql.NullString
	for _, check := range []struct {
		query string
		id    string
		dest  *sql.NullString
	}{
		{`SELECT route_id FROM workspaces WHERE id = $1`, workspaceID, &workspaceRouteID},
		{`SELECT route_id FROM channels WHERE id = $1`, channelID, &channelRouteID},
		{`SELECT route_id FROM direct_conversations WHERE id = $1`, directID, &directRouteID},
		{`SELECT route_id FROM messages WHERE id = $1`, channelRootID, &channelRootRouteID},
		{`SELECT route_id FROM messages WHERE id = $1`, directRootID, &directRootRouteID},
		{`SELECT route_id FROM messages WHERE id = $1`, replyID, &replyRouteID},
	} {
		if err := st.db.QueryRowContext(ctx, check.query, check.id).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		name   string
		value  sql.NullString
		prefix string
	}{
		{"workspace", workspaceRouteID, "T"},
		{"channel", channelRouteID, "C"},
		{"direct conversation", directRouteID, "D"},
		{"channel root", channelRootRouteID, "M"},
	} {
		if !check.value.Valid || !strings.HasPrefix(check.value.String, check.prefix) {
			t.Fatalf("legacy %s was not backfilled: %q", check.name, check.value.String)
		}
	}
	if directRootRouteID.Valid || replyRouteID.Valid {
		t.Fatalf("legacy direct roots and replies must remain uncited: direct=%q reply=%q", directRootRouteID.String, replyRouteID.String)
	}
	var markerCount int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = $1`, routeIDBackfillMarker).Scan(&markerCount); err != nil {
		t.Fatal(err)
	}
	if markerCount != 1 {
		t.Fatalf("route ID backfill completion marker = %d, want 1", markerCount)
	}
}

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
