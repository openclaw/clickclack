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
	for index := 1; index <= 5; index++ {
		if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID: root.ID,
			AuthorID:      owner.ID,
			Body:          fmt.Sprintf("reply-%d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, latest, state, err := st.GetThreadLatest(ctx, root.ID, owner.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if state.ReplyCount != 5 || len(latest) != 2 || latest[0].Body != "reply-4" || latest[1].Body != "reply-5" {
		t.Fatalf("unexpected latest thread window: state=%#v replies=%#v", state, latest)
	}
}
