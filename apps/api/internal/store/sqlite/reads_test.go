package sqlite

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func TestMarkChannelReadAndUnreadCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	// Channel with no messages: zero on every field.
	if channel.LastSeq != 0 || channel.LastReadSeq != 0 || channel.UnreadCount != 0 {
		t.Fatalf("expected zeros for empty channel, got %#v", channel)
	}

	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	for _, authorID := range []string{other.ID, owner.ID, other.ID} {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: authorID, Body: "m"}); err != nil {
			t.Fatal(err)
		}
	}

	channels, err = st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel = channels[0]
	if channel.LastSeq != 3 || channel.UnreadCount != 2 {
		t.Fatalf("expected only other-authored messages to be unread, got %#v", channel)
	}

	// Mark first 2 as read.
	receipt, event, err := st.MarkChannelRead(ctx, channel.ID, owner.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 2 || event.Type != "channel.read" {
		t.Fatalf("unexpected mark-read result: %#v / %#v", receipt, event)
	}

	channels, _ = st.ListChannels(ctx, workspace.ID, owner.ID)
	if got := channels[0].UnreadCount; got != 1 {
		t.Fatalf("expected 1 unread after marking, got %d", got)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "own after read"}); err != nil {
		t.Fatal(err)
	}
	channels, _ = st.ListChannels(ctx, workspace.ID, owner.ID)
	if got := channels[0].UnreadCount; got != 1 {
		t.Fatalf("expected own message after read pointer not to count as unread, got %d", got)
	}

	// Idempotent / monotonic: sending a smaller seq must not regress.
	receipt, event, err = st.MarkChannelRead(ctx, channel.ID, owner.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 2 || event.ID != "" {
		t.Fatalf("expected no-op when seq regresses, got %#v / %#v", receipt, event)
	}

	// Caps to channel.last_seq when caller overshoots.
	receipt, _, err = st.MarkChannelRead(ctx, channel.ID, owner.ID, 999)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 4 {
		t.Fatalf("expected cap to 4, got %d", receipt.LastReadSeq)
	}

	// Negative seq is rejected.
	if _, _, err := st.MarkChannelRead(ctx, channel.ID, owner.ID, -1); err == nil {
		t.Fatal("expected error for negative seq")
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "stranger@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkChannelRead(ctx, channel.ID, stranger.ID, 1); err == nil {
		t.Fatal("expected non-member channel read to be rejected")
	}
	if _, _, err := st.MarkChannelRead(ctx, "chn_missing", owner.ID, 1); err == nil {
		t.Fatal("expected missing channel read to be rejected")
	}
}

func TestMarkDirectReadAndUnreadCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, _ := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, _ := st.ListWorkspaces(ctx, owner.ID)
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}

	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{other.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: other.ID, Body: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "own dm"}); err != nil {
		t.Fatal(err)
	}

	dms, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dms[0].UnreadCount != 2 {
		t.Fatalf("expected only other-authored direct messages to be unread, got %d", dms[0].UnreadCount)
	}
	gotDM, err := st.GetDirectConversation(ctx, dm.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDM.ID != dm.ID || gotDM.WorkspaceID != workspace.ID || gotDM.UnreadCount != 2 || len(gotDM.Members) != 2 {
		t.Fatalf("unexpected direct conversation lookup: %#v", gotDM)
	}

	receipt, event, err := st.MarkDirectRead(ctx, dm.ID, owner.ID, 999)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 3 || event.Type != "dm.read" {
		t.Fatalf("expected capped direct read receipt, got %#v / %#v", receipt, event)
	}
	dms, _ = st.ListDirectConversations(ctx, workspace.ID, owner.ID)
	if dms[0].UnreadCount != 0 {
		t.Fatalf("expected 0 unread after read, got %d", dms[0].UnreadCount)
	}
	gotDM, err = st.GetDirectConversation(ctx, dm.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDM.LastReadSeq != 3 || gotDM.UnreadCount != 0 {
		t.Fatalf("expected read direct conversation lookup, got %#v", gotDM)
	}
	receipt, event, err = st.MarkDirectRead(ctx, dm.ID, owner.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 3 || event.ID != "" {
		t.Fatalf("expected no-op when direct seq regresses, got %#v / %#v", receipt, event)
	}
	if _, _, err := st.MarkDirectRead(ctx, dm.ID, owner.ID, -1); err == nil {
		t.Fatal("expected error for negative direct seq")
	}

	// Non-member cannot mark read.
	stranger, _ := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "s@example.com"})
	if err := st.AddWorkspaceMember(ctx, workspace.ID, stranger.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkDirectRead(ctx, dm.ID, stranger.ID, 1); err == nil {
		t.Fatal("expected non-member to be rejected")
	}
	if _, err := st.GetDirectConversation(ctx, dm.ID, stranger.ID); err == nil {
		t.Fatal("expected non-member direct conversation lookup to be rejected")
	}
	if _, err := st.GetDirectConversation(ctx, "dm_missing", owner.ID); err == nil {
		t.Fatal("expected missing direct conversation lookup to fail")
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDirectConversation(ctx, dm.ID, other.ID); err == nil {
		t.Fatal("expected former workspace member direct conversation lookup to be rejected")
	}
	if _, err := st.ListDirectMessages(ctx, dm.ID, other.ID, store.MessagePageRequest{Limit: 10}); err == nil {
		t.Fatal("expected former workspace member direct messages to be rejected")
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: other.ID, Body: "after revoke"}); err == nil {
		t.Fatal("expected former workspace member direct message send to be rejected")
	}
	if _, _, err := st.MarkDirectRead(ctx, dm.ID, other.ID, 1); err == nil {
		t.Fatal("expected former workspace member direct read to be rejected")
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	emptyDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{stranger.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, event, err = st.MarkDirectRead(ctx, emptyDM.ID, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != 0 || event.ID != "" {
		t.Fatalf("expected empty direct read to remain at zero without event, got %#v / %#v", receipt, event)
	}
	if _, _, err := st.MarkDirectRead(ctx, "dm_missing", owner.ID, 1); err == nil {
		t.Fatal("expected missing direct read to be rejected")
	}
}

func TestReadReceiptUpsertsAreMonotonic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other-monotonic@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{other.ID}})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := st.q.UpsertChannelRead(ctx, storedb.UpsertChannelReadParams{ChannelID: channel.ID, UserID: owner.ID, LastReadSeq: 5, LastReadAt: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("expected channel read insert to affect 1 row, got %d", rows)
	}
	rows, err = st.q.UpsertChannelRead(ctx, storedb.UpsertChannelReadParams{ChannelID: channel.ID, UserID: owner.ID, LastReadSeq: 3, LastReadAt: "older"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expected regressing channel read to affect 0 rows, got %d", rows)
	}
	seq, at, err := readChannelRead(ctx, st.q, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 5 || at != "newer" {
		t.Fatalf("channel read regressed: seq=%d at=%q", seq, at)
	}

	rows, err = st.q.UpsertDirectRead(ctx, storedb.UpsertDirectReadParams{ConversationID: dm.ID, UserID: owner.ID, LastReadSeq: 4, LastReadAt: "newer-dm"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("expected direct read insert to affect 1 row, got %d", rows)
	}
	rows, err = st.q.UpsertDirectRead(ctx, storedb.UpsertDirectReadParams{ConversationID: dm.ID, UserID: owner.ID, LastReadSeq: 2, LastReadAt: "older-dm"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expected regressing direct read to affect 0 rows, got %d", rows)
	}
	seq, at, err = readDirectRead(ctx, st.q, dm.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 4 || at != "newer-dm" {
		t.Fatalf("direct read regressed: seq=%d at=%q", seq, at)
	}
}
