package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func TestMutationsCreateDurableEvents(t *testing.T) {
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
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	archived := true
	updatedChannel, channelEvent, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, Name: "harbor", Archived: &archived})
	if err != nil {
		t.Fatal(err)
	}
	if updatedChannel.Name != "harbor" || updatedChannel.ArchivedAt == nil || channelEvent.Type != "channel.updated" {
		t.Fatalf("unexpected channel update: %#v %#v", updatedChannel, channelEvent)
	}
	archived = false
	updatedChannel, _, err = st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channel.ID, UserID: owner.ID, Kind: "private", Archived: &archived})
	if err != nil {
		t.Fatal(err)
	}
	if updatedChannel.Kind != "private" || updatedChannel.ArchivedAt != nil {
		t.Fatalf("unexpected channel unarchive: %#v", updatedChannel)
	}

	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "before"})
	if err != nil {
		t.Fatal(err)
	}
	updatedMessage, updateEvent, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: owner.ID, Body: "after"})
	if err != nil {
		t.Fatal(err)
	}
	if updatedMessage.Body != "after" || updatedMessage.EditedAt == nil || updateEvent.Type != "message.updated" {
		t.Fatalf("unexpected message update: %#v %#v", updatedMessage, updateEvent)
	}
	deletedMessage, deleteEvent, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deletedMessage.DeletedAt == nil || deleteEvent.Type != "message.deleted" {
		t.Fatalf("unexpected message delete: %#v %#v", deletedMessage, deleteEvent)
	}
	repeatedDelete, repeatedDeleteEvent, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if repeatedDelete.DeletedAt == nil || *repeatedDelete.DeletedAt != *deletedMessage.DeletedAt || repeatedDeleteEvent.ID != "" {
		t.Fatalf("expected repeated delete to preserve state without event, got %#v %#v", repeatedDelete, repeatedDeleteEvent)
	}
	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspaces[0].ID, UserID: owner.ID, MemberIDs: []string{second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dmMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "dm before"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: dmMessage.ID, UserID: second.ID, Body: "dm after"}); err == nil {
		t.Fatal("expected non-author DM member update to be rejected")
	}
	updatedDM, dmEvent, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: dmMessage.ID, UserID: owner.ID, Body: "dm after"})
	if err != nil {
		t.Fatal(err)
	}
	if updatedDM.DirectConversationID != dm.ID || dmEvent.ChannelID != "" {
		t.Fatalf("unexpected dm update: %#v %#v", updatedDM, dmEvent)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: dmMessage.ID, UserID: second.ID}); err == nil {
		t.Fatal("expected non-author DM member delete to be rejected")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: dmMessage.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListEventsAfter(ctx, workspaces[0].ID, owner.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	deletedEvents := 0
	for _, event := range events {
		seen[event.Type] = true
		if event.Type == "message.deleted" {
			deletedEvents++
		}
	}
	for _, eventType := range []string{"channel.updated", "message.updated", "message.deleted"} {
		if !seen[eventType] {
			t.Fatalf("missing event %s in %#v", eventType, events)
		}
	}
	if deletedEvents != 2 {
		t.Fatalf("expected one delete event per deleted message, got %d in %#v", deletedEvents, events)
	}
}

func TestGuestChannelNameIsReserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-reserved@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, owner.ID, store.WorkspaceRoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "guest"}); err == nil {
		t.Fatal("expected guest channel create to be rejected")
	}
	general, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "general-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: general.ID, UserID: owner.ID, Name: "guest"}); err == nil {
		t.Fatal("expected rename to guest to be rejected")
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestID string
	for _, channel := range channels {
		if channel.Name == store.GuestChannelName {
			guestID = channel.ID
			break
		}
	}
	if guestID == "" {
		t.Fatalf("expected internal guest channel in %#v", channels)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: guestID, UserID: owner.ID, Name: "renamed-guest"}); err == nil {
		t.Fatal("expected rename from guest to be rejected")
	}
	archived := true
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: guestID, UserID: owner.ID, Archived: &archived}); err != nil {
		t.Fatalf("expected non-rename guest channel update to remain allowed: %v", err)
	}
}

func TestUpdateWorkspaceValidatesIconUpload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner-icon@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-icon@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	third, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Third", Email: "third-icon@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, third.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other", Slug: "other-icons"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	textUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "note.txt",
		ContentType: "text/plain",
		ByteSize:    5,
		StoragePath: "memory://note.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: otherWorkspace.ID,
		OwnerID:     owner.ID,
		Filename:    "other.png",
		ContentType: "image/png",
		ByteSize:    5,
		StoragePath: "memory://other.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	imageUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "icon.png",
		ContentType: "image/png",
		ByteSize:    5,
		StoragePath: "memory://icon.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	privateMemberUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     member.ID,
		Filename:    "member-private.png",
		ContentType: "image/png",
		ByteSize:    5,
		StoragePath: "memory://member-private.png",
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, iconURL := range map[string]string{
		"missing upload":         "/api/uploads/upl_missing",
		"non-image upload":       "/api/uploads/" + textUpload.ID,
		"other workspace upload": "/api/uploads/" + otherUpload.ID,
		"other member private":   "/api/uploads/" + privateMemberUpload.ID,
	} {
		iconURL := iconURL
		t.Run(name, func(t *testing.T) {
			if _, _, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, IconURL: &iconURL}); err == nil {
				t.Fatal("expected icon validation error")
			}
		})
	}

	iconURL := "/api/uploads/" + imageUpload.ID
	updated, event, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, IconURL: &iconURL})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IconURL != iconURL || event.Type != "workspace.updated" {
		t.Fatalf("unexpected workspace icon update: %#v %#v", updated, event)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "private icon attachment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: message.ID, UploadID: imageUpload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, imageUpload.ID, third.ID); err != nil {
		t.Fatalf("expected published icon to be visible despite private attachment: %v", err)
	}
	if _, _, err := st.TransferWorkspaceOwnership(ctx, store.TransferWorkspaceOwnershipInput{
		WorkspaceID: workspace.ID, ActorUserID: owner.ID, NewOwnerUserID: member.ID,
	}); err != nil {
		t.Fatal(err)
	}
	updatedName := "Renamed by new owner"
	if _, _, err := st.UpdateWorkspace(ctx, store.UpdateWorkspaceInput{
		WorkspaceID: workspace.ID, ActorUserID: member.ID, Name: &updatedName,
	}); err != nil {
		t.Fatalf("expected new owner to preserve the previously published icon: %v", err)
	}
}

func TestWorkspaceIconMigrationUpgradesExistingData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0021_workspace_icon_url.sql")
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO users (id, display_name, handle, created_at)
		VALUES ('usr_icon_owner', 'Icon Owner', 'icon-owner', ?)`, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, route_id, name, slug, created_at)
		VALUES ('wsp_icon_upgrade', 'THQICONUPGRADE01', 'Icon Upgrade', 'icon-upgrade', ?)`, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at)
		VALUES ('wsp_icon_upgrade', 'usr_icon_owner', 'owner', ?)`, now()); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	workspace, err := st.GetWorkspace(ctx, "wsp_icon_upgrade", "usr_icon_owner")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.IconURL != "" {
		t.Fatalf("expected migrated workspace icon_url to default empty, got %#v", workspace)
	}
	if pending, err := st.ListPendingUploadCleanups(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("expected upgraded cleanup queue to be available and empty, got %#v err=%v", pending, err)
	}
}

func TestMutationsRejectInvalidInput(t *testing.T) {
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
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: owner.ID, Body: " "}); err == nil {
		t.Fatal("expected empty message update error")
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: "missing", UserID: owner.ID, Body: "x"}); err == nil {
		t.Fatal("expected missing message update error")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: "missing", UserID: owner.ID}); err == nil {
		t.Fatal("expected missing message delete error")
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: "missing", UserID: owner.ID}); err == nil {
		t.Fatal("expected missing channel error")
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channels[0].ID, UserID: outsider.ID, Name: "nope"}); err == nil {
		t.Fatal("expected outsider channel update error")
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: outsider.ID, Body: "nope"}); err == nil {
		t.Fatal("expected outsider message update error")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: outsider.ID}); err == nil {
		t.Fatal("expected outsider message delete error")
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-mutations@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channels[0].ID, UserID: member.ID, Name: "member-edit"}); err == nil {
		t.Fatal("expected non-owner member channel update error")
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: member.ID, Body: "nope"}); err == nil {
		t.Fatal("expected non-author member message update error")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: member.ID}); err == nil {
		t.Fatal("expected non-author member message delete error")
	}
	reactionMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "react"})
	if err != nil {
		t.Fatal(err)
	}
	firstReaction, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: reactionMessage.ID, UserID: owner.ID, Emoji: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if firstReaction.ID == "" || firstReaction.Type != "reaction.added" {
		t.Fatalf("unexpected first reaction event: %#v", firstReaction)
	}
	duplicateReaction, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: reactionMessage.ID, UserID: owner.ID, Emoji: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicateReaction.ID != "" {
		t.Fatalf("duplicate reaction emitted event: %#v", duplicateReaction)
	}
	removedReaction, err := st.RemoveReaction(ctx, store.CreateReactionInput{MessageID: reactionMessage.ID, UserID: owner.ID, Emoji: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if removedReaction.ID == "" || removedReaction.Type != "reaction.removed" {
		t.Fatalf("unexpected remove reaction event: %#v", removedReaction)
	}
	missingReaction, err := st.RemoveReaction(ctx, store.CreateReactionInput{MessageID: reactionMessage.ID, UserID: owner.ID, Emoji: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if missingReaction.ID != "" {
		t.Fatalf("missing reaction emitted event: %#v", missingReaction)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: owner.ID, Body: "deleted body returns"}); err == nil {
		t.Fatal("expected deleted message update error")
	}
	affected, err := st.q.UpdateMessageBody(ctx, storedb.UpdateMessageBodyParams{Body: "race edit", EditedAt: sqlText(now()), ID: message.ID})
	if err != nil {
		t.Fatal(err)
	}
	if affected != 0 {
		t.Fatalf("deleted message update affected %d rows", affected)
	}
	reloaded, err := st.GetMessage(ctx, message.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Body != "" {
		t.Fatalf("deleted message body changed: %#v", reloaded)
	}
	results, err := st.SearchMessages(ctx, workspaces[0].ID, channels[0].ID, owner.ID, "deleted body returns", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("deleted message was searchable: %#v", results)
	}
}

func TestMutationsReturnOutboxErrors(t *testing.T) {
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
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DROP TABLE events`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channels[0].ID, UserID: owner.ID, Name: "after-events"}); err == nil {
		t.Fatal("expected channel outbox error")
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: message.ID, UserID: owner.ID, Body: "after-events"}); err == nil {
		t.Fatal("expected message update outbox error")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: message.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected message delete outbox error")
	}
}
