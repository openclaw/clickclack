package postgres

import (
	"context"
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
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "root"})
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
}

func reactionSummary(reactions []store.ReactionSummary, emoji string) store.ReactionSummary {
	for _, reaction := range reactions {
		if reaction.Emoji == emoji {
			return reaction
		}
	}
	return store.ReactionSummary{}
}
