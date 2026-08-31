package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGetThreadLatestReturnsBoundedChronologicalWindow(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Thread Owner", "postgres-thread-owner@example.com")
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
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	var lastReply store.Message
	var created []store.Message
	for index := 1; index <= 5; index++ {
		lastReply, _, _, err = st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID:   root.ID,
			AuthorID:        owner.ID,
			Body:            fmt.Sprintf("reply-%d", index),
			QuotedMessageID: &root.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, lastReply)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: lastReply.ID, UserID: owner.ID, Emoji: "🔥"}); err != nil {
		t.Fatal(err)
	}

	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: root.WorkspaceID, OwnerID: owner.ID, Filename: "thread.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: t.TempDir() + "/thread.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: lastReply.ID, UploadID: upload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}

	threadResult1, err := st.GetThreadPage(ctx, root.ID, owner.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{Limit: 2}, Latest: true})

	threadRoot, latest, state := threadResult1.Root, threadResult1.Replies, threadResult1.ThreadState
	if err != nil {
		t.Fatal(err)
	}
	if len(threadRoot.Reactions) != 1 || threadRoot.Reactions[0].Emoji != "👍" {
		t.Fatalf("expected hydrated root reaction, got %#v", threadRoot.Reactions)
	}
	if state.ReplyCount != 5 || len(latest) != 2 || latest[0].Body != "reply-4" || latest[1].Body != "reply-5" {
		t.Fatalf("unexpected latest thread window: state=%#v replies=%#v", state, latest)
	}
	if len(latest[1].Reactions) != 1 || latest[1].Reactions[0].Emoji != "🔥" {
		t.Fatalf("expected hydrated reply reaction, got %#v", latest[1].Reactions)
	}
	if latest[1].QuotedMessageID == nil || *latest[1].QuotedMessageID != root.ID || latest[1].QuotedAuthor == nil || latest[1].QuotedAuthor.ID != owner.ID || len(latest[1].Attachments) != 1 {
		t.Fatalf("lost quote/attachment hydration: %#v", latest[1])
	}
	cursor := func(n int64) *int64 { return &n }
	for _, tc := range []struct {
		name           string
		request        store.MessagePageRequest
		oldest, newest int64
		older, newer   bool
	}{
		{"earliest", store.MessagePageRequest{Limit: 2}, 1, 2, false, true},
		{"before", store.MessagePageRequest{BeforeSeq: cursor(4), Limit: 2}, 2, 3, true, true},
		{"after", store.MessagePageRequest{AfterSeq: cursor(3), Limit: 2}, 4, 5, true, false},
		{"around", store.MessagePageRequest{AroundSeq: cursor(3), Limit: 3}, 2, 4, true, true},
		{"around start", store.MessagePageRequest{AroundSeq: cursor(0), Limit: 3}, 1, 3, false, true},
		{"around end", store.MessagePageRequest{AroundSeq: cursor(99), Limit: 3}, 3, 5, true, false},
		{"empty older", store.MessagePageRequest{BeforeSeq: cursor(1)}, 0, 0, false, false},
		{"empty newer", store.MessagePageRequest{AfterSeq: cursor(5)}, 0, 0, false, false},
		{"default limit", store.MessagePageRequest{Limit: 201}, 1, 5, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := st.GetThreadPage(ctx, root.ID, owner.ID, store.ThreadPageRequest{MessagePageRequest: tc.request})
			if err != nil {
				t.Fatal(err)
			}
			if page.OldestSeq != tc.oldest || page.NewestSeq != tc.newest || page.HasOlder != tc.older || page.HasNewer != tc.newer || page.ThreadState.ReplyCount != 5 {
				t.Fatalf("incorrect thread page: %#v", page)
			}
			if len(page.Replies) > 0 && (*page.Replies[0].ThreadSeq != tc.oldest || *page.Replies[len(page.Replies)-1].ThreadSeq != tc.newest) {
				t.Fatalf("incorrect reply sequences: %#v", page.Replies)
			}
		})
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: created[2].ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	deleted, err := st.GetThreadPage(ctx, root.ID, owner.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{AroundSeq: cursor(3), Limit: 1}})
	if err != nil || len(deleted.Replies) != 1 || deleted.Replies[0].DeletedAt == nil || deleted.Replies[0].Body != "" {
		t.Fatalf("tombstone missing from around page: %#v %v", deleted, err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outside", Email: "thread-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetThreadPage(ctx, root.ID, outsider.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{AfterSeq: cursor(2)}}); err == nil {
		t.Fatal("outsider read thread page")
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "thread-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{member.ID, outsider.ID} {
		if err := st.AddWorkspaceMember(ctx, root.WorkspaceID, id, "member"); err != nil {
			t.Fatal(err)
		}
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: root.WorkspaceID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dmRoot, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "private root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: dmRoot.ID, AuthorID: member.ID, Body: "private reply"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{member.ID, outsider.ID} {
		page, err := st.GetThreadPage(ctx, dmRoot.ID, id, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{AfterSeq: cursor(0)}})
		if id == outsider.ID && err == nil {
			t.Fatal("workspace-only member read DM thread")
		}
		if id == member.ID && (err != nil || len(page.Replies) != 1) {
			t.Fatalf("DM member could not page: %#v %v", page, err)
		}
	}

}
