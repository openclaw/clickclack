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
	for index := 1; index <= 5; index++ {
		lastReply, _, _, err = st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID: root.ID,
			AuthorID:      owner.ID,
			Body:          fmt.Sprintf("reply-%d", index),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: lastReply.ID, UserID: owner.ID, Emoji: "🔥"}); err != nil {
		t.Fatal(err)
	}

	threadRoot, latest, state, err := st.GetThreadLatest(ctx, root.ID, owner.ID, 2)
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
}
