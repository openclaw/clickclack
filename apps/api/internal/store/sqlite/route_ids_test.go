package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestRouteIDsCreationResolutionAndPermissions(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)

	if !hasRoutePrefix(workspace.RouteID, "T") {
		t.Fatalf("workspace route_id was not assigned: %#v", workspace)
	}
	if !hasRoutePrefix(channel.RouteID, "C") {
		t.Fatalf("channel route_id was not assigned: %#v", channel)
	}

	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other-route@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceOnly, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Workspace Only", Email: "workspace-only-route@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, workspaceOnly.ID, "member"); err != nil {
		t.Fatal(err)
	}

	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{other.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoutePrefix(dm.RouteID, "D") {
		t.Fatalf("dm route_id was not assigned: %#v", dm)
	}

	channelTarget, err := st.ResolveRouteTarget(ctx, owner.ID, workspace.RouteID, channel.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	if channelTarget.TargetType != "channel" || channelTarget.TargetID != channel.ID || channelTarget.CanonicalPath != "/app/"+workspace.RouteID+"/"+channel.RouteID {
		t.Fatalf("unexpected channel route target: %#v", channelTarget)
	}

	legacyChannelTarget, err := st.ResolveLegacyRouteTarget(ctx, owner.ID, workspace.ID, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyChannelTarget != channelTarget {
		t.Fatalf("legacy channel route did not canonicalize to public target: %#v %#v", legacyChannelTarget, channelTarget)
	}

	dmTarget, err := st.ResolveRouteTarget(ctx, other.ID, workspace.RouteID, dm.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	if dmTarget.TargetType != "direct" || dmTarget.TargetID != dm.ID {
		t.Fatalf("unexpected dm route target: %#v", dmTarget)
	}
	if _, err := st.ResolveRouteTarget(ctx, workspaceOnly.ID, workspace.RouteID, dm.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected unauthorized dm route to fail closed, got %v", err)
	}

	root, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "private thread root"})
	if err != nil {
		t.Fatal(err)
	}
	if root.RouteID != "" {
		t.Fatalf("new root message should not eagerly get M route_id: %#v", root)
	}
	if _, err := st.EnsureMessageRouteID(ctx, owner.ID, root.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected DM root citation route to be rejected, got %v", err)
	}
	root, err = st.EnsureThreadRouteID(ctx, owner.ID, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoutePrefix(root.RouteID, "M") {
		t.Fatalf("thread root route_id was not assigned lazily: %#v", root)
	}
	threadTarget, err := st.ResolveRouteTarget(ctx, other.ID, workspace.RouteID, root.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	if threadTarget.TargetType != "thread" || threadTarget.TargetID != root.ID || threadTarget.ParentType != "direct" || threadTarget.ParentID != dm.ID || threadTarget.ParentRouteID != dm.RouteID {
		t.Fatalf("unexpected thread route target: %#v", threadTarget)
	}
	if _, err := st.ResolveRouteTarget(ctx, workspaceOnly.ID, workspace.RouteID, root.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected unauthorized dm thread route to fail closed, got %v", err)
	}

	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Routes"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolveRouteTarget(ctx, owner.ID, otherWorkspace.RouteID, channel.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected wrong workspace route to fail closed, got %v", err)
	}
	channelRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "legacy wrong workspace root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolveLegacyRouteTarget(ctx, owner.ID, otherWorkspace.ID, channelRoot.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected wrong workspace legacy thread route to fail closed, got %v", err)
	}
	channelRoot, err = getMessage(ctx, st.db, channelRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if channelRoot.RouteID != "" {
		t.Fatalf("wrong workspace legacy thread route should not assign M route_id: %#v", channelRoot)
	}
}

func TestMessageRouteIDAssignmentIsConcurrentSafeAndImmutable(t *testing.T) {
	t.Parallel()
	ctx, st, owner, _, channel := seededStore(t)
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "thread root"})
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	var wg sync.WaitGroup
	results := make(chan string, callers)
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			message, err := st.EnsureMessageRouteID(ctx, owner.ID, root.ID)
			if err != nil {
				errs <- err
				return
			}
			results <- message.RouteID
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var routeID string
	for value := range results {
		if !hasRoutePrefix(value, "M") {
			t.Fatalf("unexpected thread route_id %q", value)
		}
		if routeID == "" {
			routeID = value
		} else if routeID != value {
			t.Fatalf("concurrent route assignment returned different ids: %q vs %q", routeID, value)
		}
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE messages SET route_id = ? WHERE id = ?`, "M1111111111111111", root.ID); err == nil {
		t.Fatal("expected message route_id immutability trigger to reject update")
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE channels SET route_id = ? WHERE id = ?`, "C1111111111111111", channel.ID); err == nil {
		t.Fatal("expected channel route_id immutability trigger to reject update")
	}
}

func TestRouteTargetEdgesAndThreadCreationPaths(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "route-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResolveRouteTarget(ctx, owner.ID, "", channel.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected blank workspace route to fail, got %v", err)
	}
	if _, err := st.ResolveRouteTarget(ctx, owner.ID, workspace.RouteID, "Xbad"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected bad target prefix to fail, got %v", err)
	}
	if _, err := st.ResolveRouteTarget(ctx, outsider.ID, workspace.RouteID, channel.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected non-member route to fail closed, got %v", err)
	}
	if _, err := st.ResolveLegacyRouteTarget(ctx, owner.ID, workspace.ID, "bogus"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected bad legacy target prefix to fail, got %v", err)
	}

	badReply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: channel.ID, AuthorID: owner.ID, Body: "bad"})
	if err == nil || badReply.ID != "" {
		t.Fatalf("expected non-message thread root rejection, got message=%#v err=%v", badReply, err)
	}
	channelRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "channel thread root"})
	if err != nil {
		t.Fatal(err)
	}
	if channelRoot.RouteID != "" {
		t.Fatalf("new channel root should not eagerly get route_id: %#v", channelRoot)
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: channelRoot.ID, AuthorID: owner.ID, Body: "first reply"})
	if err != nil {
		t.Fatal(err)
	}
	channelRoot, err = getMessage(ctx, st.db, channelRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoutePrefix(channelRoot.RouteID, "M") {
		t.Fatalf("thread reply path should assign root route_id: %#v", channelRoot)
	}
	channelThreadTarget, err := st.ResolveRouteTarget(ctx, owner.ID, workspace.RouteID, channelRoot.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	if channelThreadTarget.TargetType != "thread" || channelThreadTarget.ParentType != "channel" || channelThreadTarget.ParentRouteID != channel.RouteID {
		t.Fatalf("unexpected channel thread route target: %#v", channelThreadTarget)
	}
	legacyChannelThreadTarget, err := st.ResolveLegacyRouteTarget(ctx, owner.ID, workspace.ID, channelRoot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyChannelThreadTarget != channelThreadTarget {
		t.Fatalf("legacy channel thread route did not canonicalize: %#v %#v", legacyChannelThreadTarget, channelThreadTarget)
	}
	if _, err := st.EnsureThreadRouteID(ctx, owner.ID, reply.ID); err == nil {
		t.Fatal("expected reply message to be rejected as thread route root")
	}
	if _, err := st.EnsureMessageRouteID(ctx, owner.ID, reply.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected reply citation route to be rejected, got %v", err)
	}

	rootFromGetThread, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "get thread root"})
	if err != nil {
		t.Fatal(err)
	}
	threadResult1, err := st.GetThreadPage(ctx, rootFromGetThread.ID, owner.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{Limit: 0}})
	rootFromGetThread, replies, state := threadResult1.Root, threadResult1.Replies, threadResult1.ThreadState
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoutePrefix(rootFromGetThread.RouteID, "M") || len(replies) != 0 || state.RootMessageID != rootFromGetThread.ID {
		t.Fatalf("GetThread should assign route_id and return empty thread state: root=%#v replies=%#v state=%#v", rootFromGetThread, replies, state)
	}
}

func TestEnsureDefaultWorkspaceMemberCreatesRouteIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	first, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "First", Email: "default-first@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRoutePrefix(workspace.RouteID, "T") {
		t.Fatalf("default workspace route_id was not assigned: %#v", workspace)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || !hasRoutePrefix(channels[0].RouteID, "C") {
		t.Fatalf("default channel route_id was not assigned: %#v", channels)
	}

	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "default-second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	existing, err := st.EnsureDefaultWorkspaceMember(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if existing.ID != workspace.ID || existing.RouteID != workspace.RouteID {
		t.Fatalf("expected existing default workspace, got %#v want %#v", existing, workspace)
	}
	secondWorkspaces, err := st.ListWorkspaces(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondWorkspaces) != 1 || secondWorkspaces[0].ID != workspace.ID {
		t.Fatalf("expected second user membership in default workspace, got %#v", secondWorkspaces)
	}
}

func TestRouteIDFaultBranches(t *testing.T) {
	t.Parallel()
	ctx, st, _, _, _ := seededStore(t)
	if err := st.assignRouteID(ctx, "workspaces", "wsp_missing", 'T'); err != nil {
		t.Fatalf("missing row route assignment should be a no-op, got %v", err)
	}
	if err := st.assignRouteID(ctx, "missing_table", "id", 'T'); err == nil {
		t.Fatal("expected bad table assignment error")
	}
	if err := st.backfillTableRouteIDs(ctx, "users", "T", `route_id IS NULL`); err != nil {
		t.Fatalf("missing route_id column should be ignored during backfill, got %v", err)
	}
	if err := st.backfillTableRouteIDs(ctx, "missing_table", "T", `route_id IS NULL`); err == nil {
		t.Fatal("expected bad table backfill error")
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := assignRouteIDTx(ctx, tx, "workspaces", "wsp_missing", 'T'); err != nil {
		t.Fatalf("missing row tx route assignment should be a no-op, got %v", err)
	}
	if err := assignRouteIDTx(ctx, tx, "missing_table", "id", 'T'); err == nil {
		t.Fatal("expected bad table tx assignment error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestRouteIDBackfillOnlyExistingThreadRoots(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)

	threadRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "already threaded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: threadRoot.ID, AuthorID: owner.ID, Body: "reply"}); err != nil {
		t.Fatal(err)
	}
	plainRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "plain root"})
	if err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, ctx, st, `DROP TRIGGER messages_route_id_immutable`)
	mustExecSQL(t, ctx, st, `DROP TRIGGER workspaces_route_id_immutable`)
	mustExecSQL(t, ctx, st, `DROP TRIGGER channels_route_id_immutable`)
	mustExecSQL(t, ctx, st, `UPDATE messages SET route_id = NULL WHERE id IN (?, ?)`, threadRoot.ID, plainRoot.ID)
	mustExecSQL(t, ctx, st, `UPDATE workspaces SET route_id = NULL WHERE id = ?`, workspace.ID)
	mustExecSQL(t, ctx, st, `UPDATE channels SET route_id = NULL WHERE id = ?`, channel.ID)

	if err := st.backfillRouteIDs(ctx); err != nil {
		t.Fatal(err)
	}

	var threadRouteID, plainRouteID, workspaceRouteID, channelRouteID sql.NullString
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM messages WHERE id = ?`, threadRoot.ID).Scan(&threadRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM messages WHERE id = ?`, plainRoot.ID).Scan(&plainRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM workspaces WHERE id = ?`, workspace.ID).Scan(&workspaceRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM channels WHERE id = ?`, channel.ID).Scan(&channelRouteID); err != nil {
		t.Fatal(err)
	}
	if !threadRouteID.Valid || !hasRoutePrefix(threadRouteID.String, "M") {
		t.Fatalf("thread root was not backfilled: %q", threadRouteID.String)
	}
	if plainRouteID.Valid {
		t.Fatalf("plain root should not be backfilled with M route_id: %q", plainRouteID.String)
	}
	if !workspaceRouteID.Valid || !hasRoutePrefix(workspaceRouteID.String, "T") {
		t.Fatalf("workspace was not backfilled: %q", workspaceRouteID.String)
	}
	if !channelRouteID.Valid || !hasRoutePrefix(channelRouteID.String, "C") {
		t.Fatalf("channel was not backfilled: %q", channelRouteID.String)
	}
}

func TestMigrateBackfillsRouteIDsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + t.TempDir() + "/clickclack.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, routeIDMigrationName)

	const (
		ownerID      = "usr_route_backfill_owner"
		workspaceID  = "wsp_route_backfill"
		channelID    = "chn_route_backfill"
		threadRootID = "msg_route_backfill_thread"
		plainRootID  = "msg_route_backfill_plain"
	)
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES (?, 'Owner', '', '2026-01-01T00:00:00Z')`, ownerID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspaces (id, name, slug, created_at) VALUES (?, 'Route Backfill', 'route-backfill', '2026-01-01T00:00:00Z')`, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'owner', '2026-01-01T00:00:00Z')`, workspaceID, ownerID)
	mustExecSQL(t, ctx, st, `INSERT INTO channels (id, workspace_id, name, kind, created_at) VALUES (?, ?, 'general', 'public', '2026-01-01T00:00:00Z')`, channelID, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at) VALUES (?, ?, ?, NULL, ?, NULL, ?, 1, NULL, 'thread root', 'markdown', '2026-01-01T00:00:01Z')`, threadRootID, workspaceID, channelID, ownerID, threadRootID)
	mustExecSQL(t, ctx, st, `INSERT INTO thread_state (root_message_id, reply_count, last_reply_at, last_reply_author_ids_json) VALUES (?, 1, '2026-01-01T00:00:02Z', '[]')`, threadRootID)
	mustExecSQL(t, ctx, st, `INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at) VALUES (?, ?, ?, NULL, ?, NULL, ?, 2, NULL, 'plain root', 'markdown', '2026-01-01T00:00:03Z')`, plainRootID, workspaceID, channelID, ownerID, plainRootID)

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var workspaceRouteID, channelRouteID, threadRouteID, plainRouteID sql.NullString
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&workspaceRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM channels WHERE id = ?`, channelID).Scan(&channelRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM messages WHERE id = ?`, threadRootID).Scan(&threadRouteID); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM messages WHERE id = ?`, plainRootID).Scan(&plainRouteID); err != nil {
		t.Fatal(err)
	}
	if !workspaceRouteID.Valid || !hasRoutePrefix(workspaceRouteID.String, "T") {
		t.Fatalf("workspace was not backfilled: %q", workspaceRouteID.String)
	}
	if !channelRouteID.Valid || !hasRoutePrefix(channelRouteID.String, "C") {
		t.Fatalf("channel was not backfilled: %q", channelRouteID.String)
	}
	if !threadRouteID.Valid || !hasRoutePrefix(threadRouteID.String, "M") {
		t.Fatalf("thread root was not backfilled: %q", threadRouteID.String)
	}
	if plainRouteID.Valid {
		t.Fatalf("plain root should not receive a route_id during 0011 backfill: %q", plainRouteID.String)
	}
	if scalarCount(t, ctx, st, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, routeIDBackfillMarker) != 1 {
		t.Fatal("route ID backfill completion marker was not recorded")
	}

	mustExecSQL(t, ctx, st, `DROP TRIGGER workspaces_route_id_immutable`)
	mustExecSQL(t, ctx, st, `UPDATE workspaces SET route_id = NULL WHERE id = ?`, workspaceID)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT route_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&workspaceRouteID); err != nil {
		t.Fatal(err)
	}
	if workspaceRouteID.Valid {
		t.Fatalf("completed route ID backfill should not rerun on later migrations, got %q", workspaceRouteID.String)
	}
}

func TestRouteIDBackfillOnceSkipsBeforeMigrationAndAfterMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + t.TempDir() + "/clickclack.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	mustExecSQL(t, ctx, st, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`)

	if err := st.backfillRouteIDsOnce(ctx); err != nil {
		t.Fatalf("missing route-id migration should skip backfill, got %v", err)
	}
	mustExecSQL(t, ctx, st, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, routeIDMigrationName, now())
	mustExecSQL(t, ctx, st, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, routeIDBackfillMarker, now())
	if err := st.backfillRouteIDsOnce(ctx); err != nil {
		t.Fatalf("completed route-id backfill marker should skip backfill, got %v", err)
	}
}

func TestRouteIDBackfillOnceSurfacesIncompleteMigrationState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + t.TempDir() + "/clickclack.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.backfillRouteIDsOnce(ctx); err == nil {
		t.Fatal("expected missing migration table to surface an error")
	}
	mustExecSQL(t, ctx, st, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`)
	mustExecSQL(t, ctx, st, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, routeIDMigrationName, now())
	if err := st.backfillRouteIDsOnce(ctx); err == nil {
		t.Fatal("expected route-id migration marker without route tables to surface a backfill error")
	}
}

func TestRouteIDBackfillStopsOnFirstMissingRouteTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + t.TempDir() + "/clickclack.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mustExecSQL(t, ctx, st, `CREATE TABLE workspaces (id TEXT PRIMARY KEY, route_id TEXT)`)
	if err := st.backfillRouteIDs(ctx); err == nil || !strings.Contains(err.Error(), "channels") {
		t.Fatalf("expected missing channels table error, got %v", err)
	}
	mustExecSQL(t, ctx, st, `CREATE TABLE channels (id TEXT PRIMARY KEY, route_id TEXT)`)
	if err := st.backfillRouteIDs(ctx); err == nil || !strings.Contains(err.Error(), "direct_conversations") {
		t.Fatalf("expected missing direct_conversations table error, got %v", err)
	}
}

func hasRoutePrefix(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && len(value) == 17
}

func applySQLiteMigrationsBefore(t *testing.T, ctx context.Context, st *Store, cutoff string) {
	t.Helper()
	if _, err := st.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name >= cutoff {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := st.db.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, now()); err != nil {
			t.Fatalf("%s migration record: %v", name, err)
		}
	}
}
