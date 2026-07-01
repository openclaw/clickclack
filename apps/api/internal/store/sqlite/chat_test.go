package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestStoreChatThreadsSearchUploadsAndEvents(t *testing.T) {
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

	createdChannel, channelEvent, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		Name:        "Store Room",
		UserID:      owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdChannel.Name != "store-room" || channelEvent.Type != "channel.created" {
		t.Fatalf("unexpected channel create result: %#v %#v", createdChannel, channelEvent)
	}

	root, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "searchable **message**",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "message.created" || event.ChannelID != channel.ID {
		t.Fatalf("unexpected message event: %#v", event)
	}
	if root.ChannelSeq == nil || *root.ChannelSeq != 1 {
		t.Fatalf("expected first channel sequence, got %#v", root.ChannelSeq)
	}
	idempotent, idempotentEvent, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "idempotent body",
		Nonce:     "client-nonce-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Nonce != "client-nonce-1" || idempotentEvent.Type != "message.created" {
		t.Fatalf("unexpected idempotent create result: %#v %#v", idempotent, idempotentEvent)
	}
	replayed, replayEvent, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "idempotent body",
		Nonce:     "client-nonce-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != idempotent.ID || replayEvent.ID != "" {
		t.Fatalf("expected idempotent replay without event, got %#v %#v", replayed, replayEvent)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "different body",
		Nonce:     "client-nonce-1",
	}); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected nonce conflict, got %v", err)
	}

	page, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	messages := page.Messages
	if len(messages) != 2 || messages[0].ID != root.ID || messages[1].ID != idempotent.ID || messages[1].Nonce != "client-nonce-1" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	after, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{AfterSeq: int64Ptr(*root.ChannelSeq), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Messages) != 1 || after.Messages[0].ID != idempotent.ID {
		t.Fatalf("expected idempotent message after root seq, got %#v", after)
	}

	reply, state, events, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: root.ID,
		AuthorID:      owner.ID,
		Body:          "reply body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.ThreadSeq == nil || *reply.ThreadSeq != 1 || state.ReplyCount != 1 || len(events) != 2 {
		t.Fatalf("unexpected reply result: %#v %#v %#v", reply, state, events)
	}
	threadRoot, replies, threadState, err := st.GetThread(ctx, root.ID, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if threadRoot.ID != root.ID || len(replies) != 1 || threadState.ReplyCount != 1 {
		t.Fatalf("unexpected thread: %#v %#v %#v", threadRoot, replies, threadState)
	}
	threadPage, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threadPage.Messages) != 2 {
		t.Fatalf("unexpected thread page messages: %#v", threadPage.Messages)
	}
	if got := threadPage.Messages[0].ThreadState; got == nil || got.RootMessageID != root.ID || got.ReplyCount != 1 {
		t.Fatalf("expected root thread state on message page, got %#v", got)
	}
	if got := threadPage.Messages[1].ThreadState; got == nil || got.RootMessageID != idempotent.ID || got.ReplyCount != 0 {
		t.Fatalf("expected empty thread state on root without replies, got %#v", got)
	}

	results, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "searchable", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Message.ID != root.ID {
		t.Fatalf("unexpected search results: %#v", results)
	}
	eventsAfter, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, channelEvent.Cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(eventsAfter) == 0 {
		t.Fatal("expected events after channel cursor")
	}
	allEvents, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(allEvents) == 0 {
		t.Fatal("expected events with empty cursor")
	}

	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "note.txt",
		ContentType: "text/plain",
		ByteSize:    4,
		StoragePath: "/tmp/note.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotUpload, err := st.GetUpload(ctx, upload.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotUpload.ID != upload.ID || gotUpload.Filename != "note.txt" {
		t.Fatalf("unexpected upload: %#v", gotUpload)
	}
	attachEvent, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: root.ID, UploadID: upload.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	attachPayload, _ := attachEvent.Payload.(map[string]string)
	if attachEvent.Type != "message.updated" || attachPayload["message_id"] != root.ID {
		t.Fatalf("unexpected attachment event: %#v", attachEvent)
	}
	withAttachmentPage, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	withAttachment := withAttachmentPage.Messages
	if len(withAttachment[0].Attachments) != 1 {
		t.Fatalf("expected attachment on message, got %#v", withAttachment[0])
	}

	added, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "claw"})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := st.RemoveReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "claw"})
	if err != nil {
		t.Fatal(err)
	}
	if added.Type != "reaction.added" || removed.Type != "reaction.removed" {
		t.Fatalf("unexpected reaction events: %#v %#v", added, removed)
	}
}

func TestMessageSequenceAllocationIsConcurrentSafe(t *testing.T) {
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
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "seq-other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, other.ID, "member"); err != nil {
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
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "thread root"})
	if err != nil {
		t.Fatal(err)
	}

	channelSeqs := createConcurrentMessages(t, 16, func(i int) (*int64, error) {
		msg, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: fmt.Sprintf("channel %02d", i)})
		return msg.ChannelSeq, err
	})
	assertContiguousSeqs(t, channelSeqs, 2)

	directSeqs := createConcurrentMessages(t, 16, func(i int) (*int64, error) {
		msg, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: fmt.Sprintf("dm %02d", i)})
		return msg.ChannelSeq, err
	})
	assertContiguousSeqs(t, directSeqs, 1)

	threadSeqs := createConcurrentMessages(t, 16, func(i int) (*int64, error) {
		msg, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: fmt.Sprintf("reply %02d", i)})
		return msg.ThreadSeq, err
	})
	assertContiguousSeqs(t, threadSeqs, 1)
}

func createConcurrentMessages(t *testing.T, count int, create func(int) (*int64, error)) []int64 {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, count)
	seqs := make(chan int64, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			seq, err := create(i)
			if err != nil {
				errs <- err
				return
			}
			if seq == nil {
				errs <- errors.New("message sequence is nil")
				return
			}
			seqs <- *seq
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(seqs)
	for err := range errs {
		t.Fatal(err)
	}
	got := make([]int64, 0, count)
	for seq := range seqs {
		got = append(got, seq)
	}
	if len(got) != count {
		t.Fatalf("got %d sequences, want %d: %v", len(got), count, got)
	}
	return got
}

func assertContiguousSeqs(t *testing.T, got []int64, start int64) {
	t.Helper()
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	for i, seq := range got {
		want := start + int64(i)
		if seq != want {
			t.Fatalf("seq[%d] = %d, want %d; all seqs: %v", i, seq, want, got)
		}
	}
}

func TestStoreMessagePageCursors(t *testing.T) {
	t.Parallel()
	ctx, st, owner, _, channel := seededStore(t)
	for i := 1; i <= 125; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("page message %03d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	expectSeqs(t, latest.Messages, 106, 125)
	if !latest.HasOlder || latest.HasNewer || latest.OldestSeq != 106 || latest.NewestSeq != 125 {
		t.Fatalf("unexpected latest metadata: %#v", latest)
	}

	after, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{AfterSeq: int64Ptr(10), Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	expectSeqs(t, after.Messages, 11, 15)
	if !after.HasOlder || !after.HasNewer {
		t.Fatalf("unexpected after metadata: %#v", after)
	}

	before, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{BeforeSeq: int64Ptr(106), Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	expectSeqs(t, before.Messages, 101, 105)
	if !before.HasOlder || !before.HasNewer {
		t.Fatalf("unexpected before metadata: %#v", before)
	}

	around, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{AroundSeq: int64Ptr(60), Limit: 9})
	if err != nil {
		t.Fatal(err)
	}
	expectSeqs(t, around.Messages, 56, 64)
	if !around.HasOlder || !around.HasNewer {
		t.Fatalf("unexpected around metadata: %#v", around)
	}

	if _, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{BeforeSeq: int64Ptr(20), AfterSeq: int64Ptr(10)}); !errors.Is(err, store.ErrInvalidMessagePage) {
		t.Fatalf("expected invalid page request, got %v", err)
	}
}

func TestStoreMessagePageLargeHistorySmoke(t *testing.T) {
	if os.Getenv("CLICKCLACK_LARGE_HISTORY") != "1" {
		t.Skip("set CLICKCLACK_LARGE_HISTORY=1 to run the 100k-message paging smoke")
	}
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)
	start := time.Now()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES (?, ?, ?, NULL, ?, NULL, ?, ?, NULL, ?, 'markdown', ?)`)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	createdAt := now()
	for i := 1; i <= 100000; i++ {
		id := fmt.Sprintf("msg_large_%06d", i)
		if _, err := stmt.ExecContext(ctx, id, workspace.ID, channel.ID, owner.ID, id, i, fmt.Sprintf("large history %06d", i), createdAt); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded 100k messages in %s", time.Since(start))

	for _, tc := range []struct {
		name        string
		req         store.MessagePageRequest
		first, last int64
	}{
		{"latest", store.MessagePageRequest{Limit: 50}, 99951, 100000},
		{"before", store.MessagePageRequest{BeforeSeq: int64Ptr(50000), Limit: 50}, 49950, 49999},
		{"after", store.MessagePageRequest{AfterSeq: int64Ptr(50000), Limit: 50}, 50001, 50050},
		{"around", store.MessagePageRequest{AroundSeq: int64Ptr(50000), Limit: 51}, 49975, 50025},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			page, err := st.ListMessages(ctx, channel.ID, owner.ID, tc.req)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s page loaded in %s", tc.name, time.Since(start))
			expectSeqs(t, page.Messages, tc.first, tc.last)
		})
	}
}

func TestStoreAccessErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "out@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "private"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspaces[0].ID, OwnerID: owner.ID, Filename: "x", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}

	errorCases := []struct {
		name string
		fn   func() error
	}{
		{"list workspaces outsider empty ok", func() error {
			items, err := st.ListWorkspaces(ctx, outsider.ID)
			if err != nil {
				return err
			}
			if len(items) != 0 {
				t.Fatalf("expected no workspaces for outsider, got %#v", items)
			}
			return nil
		}},
		{"get workspace denied", func() error {
			_, err := st.GetWorkspace(ctx, workspaces[0].ID, outsider.ID)
			return err
		}},
		{"list channels denied", func() error {
			_, err := st.ListChannels(ctx, workspaces[0].ID, outsider.ID)
			return err
		}},
		{"list messages denied", func() error {
			_, err := st.ListMessages(ctx, channels[0].ID, outsider.ID, store.MessagePageRequest{Limit: 10})
			return err
		}},
		{"get message denied", func() error {
			_, err := st.GetMessage(ctx, root.ID, outsider.ID)
			return err
		}},
		{"create message denied", func() error {
			_, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: outsider.ID, Body: "x"})
			return err
		}},
		{"thread denied", func() error {
			_, _, _, err := st.GetThread(ctx, root.ID, outsider.ID, 10)
			return err
		}},
		{"reply denied", func() error {
			_, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: outsider.ID, Body: "x"})
			return err
		}},
		{"update message denied", func() error {
			_, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: root.ID, UserID: outsider.ID, Body: "x"})
			return err
		}},
		{"delete message denied", func() error {
			_, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: root.ID, UserID: outsider.ID})
			return err
		}},
		{"add reaction denied", func() error {
			_, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: outsider.ID, Emoji: "x"})
			return err
		}},
		{"remove reaction denied", func() error {
			_, err := st.RemoveReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: outsider.ID, Emoji: "x"})
			return err
		}},
		{"events denied", func() error {
			_, err := st.ListEventsAfter(ctx, workspaces[0].ID, outsider.ID, "", 10)
			return err
		}},
		{"upload denied", func() error {
			_, err := st.GetUpload(ctx, upload.ID, outsider.ID)
			return err
		}},
		{"attach denied", func() error {
			_, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: root.ID, UploadID: upload.ID, UserID: outsider.ID})
			return err
		}},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if tc.name == "list workspaces outsider empty ok" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStoreDirectMessagesAndUserLookup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.GetUser(ctx, owner.ID); err != nil || got.ID != owner.ID {
		t.Fatalf("unexpected user lookup: %#v err=%v", got, err)
	}
	other, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Third", Email: "third@example.com"})
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
	if err := st.AddWorkspaceMember(ctx, workspace.ID, third.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{"", other.ID, other.ID},
	}); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{other.ID, third.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dm.Members) != 3 {
		t.Fatalf("expected three dm members, got %#v", dm.Members)
	}
	list, err := st.ListDirectConversations(ctx, workspace.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected two dm conversations for other member, got %#v", list)
	}
	tooManyMemberIDs := make([]string, 0, store.MaxDirectConversationMembers)
	for i := 0; i < store.MaxDirectConversationMembers; i++ {
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: fmt.Sprintf("DM Extra %02d", i), Email: fmt.Sprintf("dm-extra-%02d@example.com", i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, user.ID, "member"); err != nil {
			t.Fatal(err)
		}
		tooManyMemberIDs = append(tooManyMemberIDs, user.ID)
	}
	if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   tooManyMemberIDs,
	}); err == nil {
		t.Fatal("expected overlarge direct conversation to be rejected")
	}
	msg, event, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       other.ID,
		Body:           " direct hello ",
		Nonce:          "dm-nonce-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.DirectConversationID != dm.ID || event.Type != "message.created" || event.ChannelID != "" {
		t.Fatalf("unexpected direct message result: %#v %#v", msg, event)
	}
	replayedDM, replayedDMEvent, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       other.ID,
		Body:           "direct hello",
		Nonce:          "dm-nonce-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayedDM.ID != msg.ID || replayedDMEvent.ID != "" {
		t.Fatalf("expected idempotent dm replay without event, got %#v %#v", replayedDM, replayedDMEvent)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       other.ID,
		Body:           "different direct hello",
		Nonce:          "dm-nonce-1",
	}); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected dm nonce conflict, got %v", err)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       other.ID,
		Body:           "too long nonce",
		Nonce:          strings.Repeat("n", 129),
	}); err == nil {
		t.Fatal("expected too-long dm nonce to be rejected")
	}
	page, err := st.ListDirectMessages(ctx, dm.ID, third.ID, store.MessagePageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	messages := page.Messages
	if len(messages) != 1 || messages[0].Body != "direct hello" {
		t.Fatalf("unexpected direct messages: %#v", messages)
	}
	after, err := st.ListDirectMessages(ctx, dm.ID, third.ID, store.MessagePageRequest{AfterSeq: int64Ptr(*messages[0].ChannelSeq), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Messages) != 0 {
		t.Fatalf("expected no direct messages after seq, got %#v", after)
	}
	gotDM, err := st.GetMessage(ctx, msg.ID, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotDM.ID != msg.ID || gotDM.DirectConversationID != dm.ID {
		t.Fatalf("unexpected direct message lookup: %#v", gotDM)
	}
	workspaceOnly, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Workspace Only", Email: "workspace-only@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, workspaceOnly.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMessage(ctx, msg.ID, workspaceOnly.ID); err == nil {
		t.Fatal("expected direct message lookup to reject workspace member outside the DM")
	}

	errorCases := []struct {
		name string
		fn   func() error
	}{
		{"single member", func() error {
			_, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID})
			return err
		}},
		{"nonmember create dm", func() error {
			outside, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outside", Email: "outside@example.com"})
			if err != nil {
				return err
			}
			_, err = st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{outside.ID}})
			return err
		}},
		{"empty dm body", func() error {
			_, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID})
			return err
		}},
		{"missing dm create", func() error {
			_, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: "dm_missing", AuthorID: owner.ID, Body: "x"})
			return err
		}},
		{"missing dm", func() error {
			_, err := st.ListDirectMessages(ctx, "dm_missing", owner.ID, store.MessagePageRequest{Limit: 10})
			return err
		}},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStoreBranchCases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.EnsureBootstrap(ctx, "Ignored", "ignored@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != owner.ID {
		t.Fatalf("expected existing bootstrap user, got %#v", again)
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

	secondWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defaultChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: secondWorkspace.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if defaultChannel.Name != "general" || defaultChannel.Kind != "public" {
		t.Fatalf("unexpected default channel: %#v", defaultChannel)
	}
	otherUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: secondWorkspace.ID, OwnerID: owner.ID, Filename: "other", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/other"})
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root for branches"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: root.ID, UploadID: otherUpload.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected mismatched upload workspace error")
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.GetThread(ctx, reply.ID, owner.ID, 10); err == nil {
		t.Fatal("expected reply-as-root error")
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: reply.ID, AuthorID: owner.ID, Body: "nested"}); err == nil {
		t.Fatal("expected nested reply error")
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID}); err == nil {
		t.Fatal("expected empty reply body error")
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: "chn_missing", AuthorID: owner.ID, Body: "x"}); err == nil {
		t.Fatal("expected missing channel error")
	}
	if _, err := st.ListMessages(ctx, "chn_missing", owner.ID, store.MessagePageRequest{Limit: 10}); err == nil {
		t.Fatal("expected missing channel list error")
	}
	if results, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "missingterm", 999); err != nil || len(results) != 0 {
		t.Fatalf("expected no search results, got %#v err=%v", results, err)
	}
	if _, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 999); err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Branch Outsider", Email: "branch-out@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateInvite(ctx, workspace.ID, outsider.ID); err == nil {
		t.Fatal("expected invite membership error")
	}
	if _, err := st.SearchMessages(ctx, workspace.ID, "", outsider.ID, "root", 10); err == nil {
		t.Fatal("expected search membership error")
	}

	firstLink, err := st.CreateMagicLink(ctx, "reuse@example.com", "Reuse One")
	if err != nil {
		t.Fatal(err)
	}
	firstUser, _, err := st.ConsumeMagicLink(ctx, firstLink.Token)
	if err != nil {
		t.Fatal(err)
	}
	secondLink, err := st.CreateMagicLink(ctx, "reuse@example.com", "Reuse Two")
	if err != nil {
		t.Fatal(err)
	}
	secondUser, _, err := st.ConsumeMagicLink(ctx, secondLink.Token)
	if err != nil {
		t.Fatal(err)
	}
	if firstUser.ID != secondUser.ID {
		t.Fatalf("expected reused magic user, got %s and %s", firstUser.ID, secondUser.ID)
	}
	expired, err := st.CreateMagicLink(ctx, "expired@example.com", "Expired")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE auth_magic_links SET expires_at = '2000-01-01T00:00:00Z' WHERE token_hash = ?`, tokenHash(expired.Token)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ConsumeMagicLink(ctx, expired.Token); err == nil {
		t.Fatal("expected expired magic link error")
	}
}

func int64Ptr(v int64) *int64 { return &v }

func expectSeqs(t *testing.T, messages []store.Message, first, last int64) {
	t.Helper()
	wantLen := int(last - first + 1)
	if len(messages) != wantLen {
		t.Fatalf("expected %d messages from seq %d to %d, got %d: %#v", wantLen, first, last, len(messages), messages)
	}
	for i, message := range messages {
		want := first + int64(i)
		if message.ChannelSeq == nil || *message.ChannelSeq != want {
			t.Fatalf("message %d: expected seq %d, got %#v", i, want, message.ChannelSeq)
		}
	}
}
