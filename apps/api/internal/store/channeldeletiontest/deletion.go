// Package channeldeletiontest checks channel deletion against each SQL backend.
package channeldeletiontest

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/storetest"
)

// Hooks give the shared checks access to backend-specific SQL.
type Hooks struct {
	// OwnedRows counts rows that must disappear with the channel, keyed by a
	// readable label such as "reactions" or "search rows".
	OwnedRows func(t *testing.T, channelID string, messageIDs []string) map[string]int64
}

type fixture struct {
	owner      store.User
	member     store.User
	workspace  store.Workspace
	doomed     store.Channel
	kept       store.Channel
	root       store.Message
	reply      store.Message
	keptRoot   store.Message
	exclusive  store.Upload
	shared     store.Upload
	icon       store.Upload
	directRoot store.Message
	rootEvent  store.Event
}

func DeleteChannelRemovesOwnedContent(t *testing.T, st store.Store, hooks Hooks) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)

	preview, err := st.PreviewChannelDeletion(ctx, f.doomed.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := store.ChannelDeletionCounts{Messages: 1, ThreadReplies: 1, Pins: 1, Topics: 1, Files: 1, FileBytes: f.exclusive.ByteSize}
	if preview.Channel.ID != f.doomed.ID || preview.Counts != wantCounts || preview.Blocker != "" {
		t.Fatalf("preview = %#v, want counts %#v", preview, wantCounts)
	}
	before, err := st.LatestEventCursor(ctx, f.workspace.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	deletion, err := st.DeleteChannel(ctx, f.doomed.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deletion.Channel.ID != f.doomed.ID || deletion.Counts != wantCounts {
		t.Fatalf("deletion = %#v", deletion)
	}
	payload, _ := deletion.Event.Payload.(map[string]string)
	if deletion.Event.Type != "channel.deleted" || deletion.Event.ChannelID != "" || payload["channel_id"] != f.doomed.ID || payload["deleted_by"] != f.owner.ID || deletion.Event.Cursor <= before {
		t.Fatalf("deletion event = %#v, previous cursor %q", deletion.Event, before)
	}
	if len(deletion.Cleanups) != 1 || deletion.Cleanups[0].StoragePath != f.exclusive.StoragePath {
		t.Fatalf("cleanups = %#v, want only %q", deletion.Cleanups, f.exclusive.StoragePath)
	}

	if _, err := st.GetChannel(ctx, f.doomed.ID, f.owner.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted channel lookup error = %v", err)
	}
	for _, id := range []string{f.root.ID, f.reply.ID} {
		if _, err := st.GetMessage(ctx, id, f.owner.ID); err == nil {
			t.Fatalf("message %s survived channel deletion", id)
		}
	}
	if _, err := st.GetUpload(ctx, f.exclusive.ID, f.owner.ID); err == nil {
		t.Fatal("upload used only by the deleted channel survived")
	}
	if _, err := st.GetUpload(ctx, f.shared.ID, f.owner.ID); err != nil {
		t.Fatalf("upload still attached in another channel was removed: %v", err)
	}
	if workspace, err := st.GetWorkspace(ctx, f.workspace.ID, f.owner.ID); err != nil || workspace.IconURL != "/api/uploads/"+f.icon.ID {
		t.Fatalf("workspace icon changed: %#v, %v", workspace, err)
	}
	if _, err := st.GetUpload(ctx, f.icon.ID, f.owner.ID); err != nil {
		t.Fatalf("workspace icon upload was removed: %v", err)
	}
	for _, id := range []string{f.keptRoot.ID, f.directRoot.ID} {
		if _, err := st.GetMessage(ctx, id, f.owner.ID); err != nil {
			t.Fatalf("message %s outside the channel was removed: %v", id, err)
		}
	}
	topics, err := st.ListTopics(ctx, f.workspace.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		if topic.ChannelID == f.doomed.ID {
			t.Fatalf("channel topic survived: %#v", topic)
		}
	}
	search, err := st.SearchMessagePage(ctx, store.SearchPageRequest{WorkspaceID: f.workspace.ID, UserID: f.owner.ID, Query: "zebracrossing", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range search.Results {
		if hit.ChannelID == f.doomed.ID {
			t.Fatalf("search still returns deleted channel content: %#v", hit)
		}
	}
	// A client whose cursor points at one of the channel's events must still
	// replay forward to the deletion instead of losing its place.
	if exists, err := st.EventCursorExists(ctx, f.workspace.ID, f.member.ID, f.rootEvent.Cursor); err != nil || !exists {
		t.Fatalf("cursor of a deleted channel's event exists = %t, %v", exists, err)
	}
	replay, err := st.ListEventsAfter(ctx, f.workspace.ID, f.member.ID, f.rootEvent.Cursor, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) == 0 || replay[len(replay)-1].ID != deletion.Event.ID {
		t.Fatalf("replay from a deleted channel's event did not reach the deletion: %#v", replay)
	}
	for label, count := range hooks.OwnedRows(t, f.doomed.ID, []string{f.root.ID, f.reply.ID}) {
		if count != 0 {
			t.Fatalf("%s left behind: %d", label, count)
		}
	}
}

func DeleteChannelEnforcesGuardRails(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, st)

	if _, err := st.PreviewChannelDeletion(ctx, f.doomed.ID, f.member.ID); !errors.Is(err, store.ErrWorkspaceOwnerRequired) {
		t.Fatalf("member preview error = %v", err)
	}
	if _, err := st.DeleteChannel(ctx, f.doomed.ID, f.member.ID); !errors.Is(err, store.ErrWorkspaceOwnerRequired) {
		t.Fatalf("member delete error = %v", err)
	}
	if _, err := st.DeleteChannel(ctx, "chn_missing", f.owner.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing channel delete error = %v", err)
	}

	solo, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Solo room"}, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	only, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: solo.ID, UserID: f.owner.ID, Name: "only"})
	if err != nil {
		t.Fatal(err)
	}
	if preview, err := st.PreviewChannelDeletion(ctx, only.ID, f.owner.ID); err != nil || preview.Blocker != store.ChannelDeletionBlockedLastChannel {
		t.Fatalf("last channel preview = %#v, %v", preview, err)
	}
	if _, err := st.DeleteChannel(ctx, only.ID, f.owner.ID); !errors.Is(err, store.ErrLastChannel) {
		t.Fatalf("last channel delete error = %v", err)
	}

	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "channel-delete-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	guests, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, guests.ID, f.owner.ID, store.WorkspaceRoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: guests.ID, UserID: f.owner.ID, Name: "lounge"}); err != nil {
		t.Fatal(err)
	}
	guestChannels, err := st.ListChannels(ctx, guests.ID, f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, channel := range guestChannels {
		_, err := st.DeleteChannel(ctx, channel.ID, f.owner.ID)
		switch channel.Name {
		case store.GuestChannelName, "general":
			if !errors.Is(err, store.ErrProvisionedChannel) {
				t.Fatalf("provisioned #%s delete error = %v", channel.Name, err)
			}
		default:
			if err != nil {
				t.Fatalf("ordinary guest workspace channel #%s delete error = %v", channel.Name, err)
			}
		}
	}

	archived := true
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: f.kept.ID, UserID: f.owner.ID, Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteChannel(ctx, f.kept.ID, f.owner.ID); err != nil {
		t.Fatalf("archived channel delete error = %v", err)
	}
}

func newFixture(t *testing.T, st store.Store) fixture {
	t.Helper()
	ctx := context.Background()
	var f fixture
	var err error
	if f.owner, err = st.EnsureBootstrap(ctx, "Owner", "channel-delete-owner@example.com"); err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, f.owner.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("bootstrap workspaces = %#v, %v", workspaces, err)
	}
	f.workspace = workspaces[0]
	if f.member, err = st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "channel-delete-member@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, f.workspace.ID, f.member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if f.doomed, _, err = st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: f.workspace.ID, UserID: f.owner.ID, Name: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if f.kept, _, err = st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: f.workspace.ID, UserID: f.owner.ID, Name: "kept"}); err != nil {
		t.Fatal(err)
	}
	topic, err := st.CreateTopic(ctx, store.CreateTopicInput{WorkspaceID: f.workspace.ID, ChannelID: f.doomed.ID, Name: "launch", CreatedBy: f.owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if f.root, f.rootEvent, err = st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.doomed.ID, AuthorID: f.owner.ID, Body: "zebracrossing root", TopicID: topic.ID}); err != nil {
		t.Fatal(err)
	}
	if f.reply, _, _, err = st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: f.root.ID, AuthorID: f.member.ID, Body: "zebracrossing reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: f.root.ID, UserID: f.member.ID, Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PinMessage(ctx, f.doomed.ID, f.root.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkChannelRead(ctx, f.doomed.ID, f.member.ID, *f.root.ChannelSeq); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{ChannelID: f.doomed.ID, UserID: f.member.ID, Preference: store.ChannelNotifyMuted}); err != nil {
		t.Fatal(err)
	}
	f.exclusive = createUpload(t, st, f, "only.txt", "text/plain", 11)
	f.shared = createUpload(t, st, f, "shared.txt", "text/plain", 13)
	f.icon = createUpload(t, st, f, "icon.png", "image/png", 17)
	for _, upload := range []store.Upload{f.exclusive, f.shared, f.icon} {
		if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: f.root.ID, UploadID: upload.ID, UserID: f.owner.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if f.keptRoot, _, err = st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.kept.ID, AuthorID: f.owner.ID, Body: "kept zebracrossing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: f.keptRoot.ID, UploadID: f.shared.ID, UserID: f.owner.ID}); err != nil {
		t.Fatal(err)
	}
	iconURL := "/api/uploads/" + f.icon.ID
	if _, _, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{WorkspaceID: f.workspace.ID, ActorUserID: f.owner.ID, IconURL: &iconURL}); err != nil {
		t.Fatal(err)
	}
	conversation, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: f.workspace.ID, UserID: f.owner.ID, MemberIDs: []string{f.member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if f.directRoot, _, err = st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: conversation.ID, AuthorID: f.owner.ID, Body: "direct zebracrossing"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func createUpload(t *testing.T, st store.Store, f fixture, filename, contentType string, size int64) store.Upload {
	t.Helper()
	upload, err := storetest.CreateUpload(context.Background(), st, store.CreateUploadInput{
		WorkspaceID: f.workspace.ID,
		OwnerID:     f.owner.ID,
		Filename:    filename,
		ContentType: contentType,
		ByteSize:    size,
		StoragePath: "channel-delete/" + filename,
	})
	if err != nil {
		t.Fatal(err)
	}
	return upload
}
