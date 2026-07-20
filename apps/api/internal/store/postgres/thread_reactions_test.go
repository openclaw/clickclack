package postgres

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGetThreadHydratesReactions(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Reaction Owner", "postgres-reaction-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Reaction Member", Email: "postgres-reaction-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "root", Nonce: "reaction-root"})
	if err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: member.ID, Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: member.ID, Emoji: "👀"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: reply.ID, UserID: owner.ID, Emoji: "🔥"}); err != nil {
		t.Fatal(err)
	}

	threadRoot, replies, _, err := st.GetThread(ctx, root.ID, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(threadRoot.Reactions) != 2 {
		t.Fatalf("expected hydrated root reaction, got %#v", threadRoot.Reactions)
	}
	if got := reactionSummary(threadRoot.Reactions, "👍"); got.Count != 2 || !got.ReactedByMe {
		t.Fatalf("expected owner summary for thumbs-up, got %#v", got)
	}
	if got := reactionSummary(threadRoot.Reactions, "👀"); got.Count != 1 || got.ReactedByMe {
		t.Fatalf("expected owner summary for eyes, got %#v", got)
	}
	if len(replies) != 1 || len(replies[0].Reactions) != 1 || replies[0].Reactions[0].Emoji != "🔥" || !replies[0].Reactions[0].ReactedByMe {
		t.Fatalf("expected hydrated reply reaction, got %#v", replies)
	}
	byNonce, err := st.GetMessageByNonce(ctx, owner.ID, "reaction-root")
	if err != nil {
		t.Fatal(err)
	}
	if got := reactionSummary(byNonce.Reactions, "👍"); got.Count != 2 || !got.ReactedByMe {
		t.Fatalf("expected nonce lookup to hydrate reactions, got %#v", byNonce.Reactions)
	}

	concurrent, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	users := make([]store.User, 0, writers)
	users = append(users, owner, member)
	for index := len(users); index < writers; index++ {
		user, err := st.CreateUser(ctx, store.CreateUserInput{
			DisplayName: fmt.Sprintf("Reaction Writer %d", index),
			Email:       fmt.Sprintf("postgres-reaction-writer-%d@example.com", index),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, user.ID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
	}

	start := make(chan struct{})
	counts := make(chan int64, writers)
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for _, user := range users {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			<-start
			event, err := st.AddReaction(ctx, store.CreateReactionInput{
				MessageID: concurrent.ID,
				UserID:    userID,
				Emoji:     "⚡",
			})
			if err != nil {
				errs <- err
				return
			}
			payload, ok := event.Payload.(map[string]any)
			if !ok {
				errs <- fmt.Errorf("unexpected reaction payload %#v", event.Payload)
				return
			}
			count, ok := payload["count"].(int64)
			if !ok {
				errs <- fmt.Errorf("unexpected reaction count %#v", payload["count"])
				return
			}
			counts <- count
		}(user.ID)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(counts)
	for err := range errs {
		t.Fatal(err)
	}
	gotCounts := make([]int, 0, writers)
	for count := range counts {
		gotCounts = append(gotCounts, int(count))
	}
	sort.Ints(gotCounts)
	for index, count := range gotCounts {
		if want := index + 1; count != want {
			t.Fatalf("expected serialized authoritative counts 1..%d, got %#v", writers, gotCounts)
		}
	}
}

func reactionSummary(reactions []store.ReactionSummary, emoji string) store.ReactionSummary {
	for _, reaction := range reactions {
		if reaction.Emoji == emoji {
			return reaction
		}
	}
	return store.ReactionSummary{}
}
