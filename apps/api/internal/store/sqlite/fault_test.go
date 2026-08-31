package sqlite

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestStoreFaultBranches(t *testing.T) {
	t.Parallel()
	t.Run("event insert failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		if _, err := st.db.ExecContext(ctx, `DROP TABLE events`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "events-down"}); err == nil {
			t.Fatal("expected event insert failure")
		}
	})

	t.Run("thread state failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, _, channel := seededStore(t)
		root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE thread_state`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetThreadPage(ctx, root.ID, owner.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{Limit: 10}}); err == nil {
			t.Fatal("expected get thread state failure")
		}
		if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: "reply"}); err == nil {
			t.Fatal("expected update thread state failure")
		}
	})

	t.Run("attachment hydration failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, _, channel := seededStore(t)
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE message_attachments`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10}); err == nil {
			t.Fatal("expected attachment hydration failure")
		}
	})

	t.Run("direct conversation query failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		if _, err := st.db.ExecContext(ctx, `DROP TABLE direct_conversation_members`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID); err == nil {
			t.Fatal("expected direct conversation query failure")
		}
		if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{owner.ID}}); err == nil {
			t.Fatal("expected direct conversation membership failure")
		}
	})

	t.Run("direct member hydration failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-hydrate@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE users`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID); err == nil {
			t.Fatal("expected direct member hydration failure")
		}
	})

	t.Run("upload query failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, channel := seededStore(t)
		root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root"})
		if err != nil {
			t.Fatal(err)
		}
		upload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "x", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/x"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE uploads`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.GetUpload(ctx, upload.ID, owner.ID); err == nil {
			t.Fatal("expected get upload failure")
		}
		if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: root.ID, UploadID: upload.ID, UserID: owner.ID}); err == nil {
			t.Fatal("expected attach upload failure")
		}
	})

	t.Run("bad magic link expiration", func(t *testing.T) {
		t.Parallel()
		ctx, st, _, _, _ := seededStore(t)
		link, err := st.CreateMagicLink(ctx, "bad-expiry@example.com", "Bad")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `UPDATE auth_magic_links SET expires_at = 'bad' WHERE token_hash = ?`, tokenHash(link.Token)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.ConsumeMagicLink(ctx, link.Token); err == nil {
			t.Fatal("expected bad expiry error")
		}
		if _, _, err := st.ConsumeMagicLink(ctx, "missing"); err == nil {
			t.Fatal("expected missing link error")
		}
	})

	t.Run("magic link session failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, _, _, _ := seededStore(t)
		link, err := st.CreateMagicLink(ctx, "session-fail@example.com", "Session")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE sessions`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.ConsumeMagicLink(ctx, link.Token); err == nil {
			t.Fatal("expected session create failure")
		}
	})

	t.Run("duplicate local identity", func(t *testing.T) {
		t.Parallel()
		ctx, st, _, _, _ := seededStore(t)
		if _, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "One", Email: "dupe@example.com"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Two", Email: "dupe@example.com"}); err == nil {
			t.Fatal("expected duplicate identity error")
		}
	})

	t.Run("message table failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, _, channel := seededStore(t)
		if _, err := st.db.ExecContext(ctx, `DROP TABLE messages`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListMessages(ctx, channel.ID, owner.ID, store.MessagePageRequest{Limit: 10}); err == nil {
			t.Fatal("expected list messages failure")
		}
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected create message sequence failure")
		}
		if _, err := st.GetThreadPage(ctx, "msg_missing", owner.ID, store.ThreadPageRequest{MessagePageRequest: store.MessagePageRequest{Limit: 10}}); err == nil {
			t.Fatal("expected get thread message failure")
		}
	})

	t.Run("message transaction failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, _, channel := seededStore(t)
		if _, err := st.db.ExecContext(ctx, `DROP TABLE thread_state`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected message thread-state failure")
		}
	})

	t.Run("message event failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, channel := seededStore(t)
		if _, err := st.db.ExecContext(ctx, `DROP TABLE events`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected message event failure")
		}
		if _, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 10); err == nil {
			t.Fatal("expected list events failure")
		}
	})

	t.Run("channel and workspace write failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "outsider@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: outsider.ID, Name: "denied"}); err == nil {
			t.Fatal("expected channel membership failure")
		}
		if _, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "No Owner"}, "usr_missing"); err == nil {
			t.Fatal("expected workspace member foreign-key failure")
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE channels`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListChannels(ctx, workspace.ID, owner.ID); err == nil {
			t.Fatal("expected list channels failure")
		}
	})

	t.Run("reaction failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, _, channel := seededStore(t)
		root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root"})
		if err != nil {
			t.Fatal(err)
		}
		outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "out@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: outsider.ID, Emoji: "x"}); err == nil {
			t.Fatal("expected reaction membership failure")
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE reactions`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: root.ID, UserID: owner.ID, Emoji: "x"}); err == nil {
			t.Fatal("expected reaction write failure")
		}
	})

	t.Run("direct message failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
			t.Fatal(err)
		}
		dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
		if err != nil {
			t.Fatal(err)
		}
		outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "dmout@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: outsider.ID, Body: "x"}); err == nil {
			t.Fatal("expected direct message membership failure")
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE messages`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListDirectMessages(ctx, dm.ID, owner.ID, store.MessagePageRequest{Limit: 10}); err == nil {
			t.Fatal("expected direct list failure")
		}
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected direct message sequence failure")
		}
	})

	t.Run("direct message transaction failures", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "direct-tx@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
			t.Fatal(err)
		}
		dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE thread_state`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected direct thread-state failure")
		}
	})

	t.Run("direct message event failure", func(t *testing.T) {
		t.Parallel()
		ctx, st, owner, workspace, _ := seededStore(t)
		member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "direct-event@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
			t.Fatal(err)
		}
		dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `DROP TABLE events`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "x"}); err == nil {
			t.Fatal("expected direct event failure")
		}
	})
}

func seededStore(t *testing.T) (context.Context, *Store, store.User, store.Workspace, store.Channel) {
	t.Helper()
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
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, st, owner, workspaces[0], channels[0]
}
