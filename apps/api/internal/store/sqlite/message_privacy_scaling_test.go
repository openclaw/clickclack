package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestMessagePrivacyScalingPrivacyAndDMThreads(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "DM Member", Email: "dm-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaceOnly, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Workspace Only", Email: "workspace-only-hardening@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, workspaceOnly.ID, "member"); err != nil {
		t.Fatal(err)
	}

	publicMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "public searchable channel text",
	})
	if err != nil {
		t.Fatal(err)
	}
	quotedPublicID := publicMessage.ID
	quotedPublicMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID:       channel.ID,
		AuthorID:        owner.ID,
		Body:            "quotechannel visible text",
		QuotedMessageID: &quotedPublicID,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "other-hardening"})
	if err != nil {
		t.Fatal(err)
	}
	otherChannelMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: otherChannel.ID,
		AuthorID:  owner.ID,
		Body:      "public searchable other channel text",
	})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dmMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "private searchable dm secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	dmNonceMessage, dmNonceEvent, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "dm nonce message",
		Nonce:          "dm-nonce-coverage",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dmNonceEvent.ID == "" {
		t.Fatalf("expected direct nonce create event, got %#v", dmNonceEvent)
	}
	dmNonceReplay, dmReplayEvent, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "dm nonce message",
		Nonce:          "dm-nonce-coverage",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dmNonceReplay.ID != dmNonceMessage.ID || dmReplayEvent.ID != "" {
		t.Fatalf("expected direct nonce replay without event, got %#v %#v", dmNonceReplay, dmReplayEvent)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: dm.ID,
		AuthorID:       owner.ID,
		Body:           "dm nonce conflict",
		Nonce:          "dm-nonce-coverage",
	}); !errors.Is(err, store.ErrClientNonceConflict) {
		t.Fatalf("expected direct nonce conflict, got %v", err)
	}
	quotedID := dmNonceMessage.ID
	quotedDM, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID:  dm.ID,
		AuthorID:        member.ID,
		Body:            "dm quote message",
		QuotedMessageID: &quotedID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if quotedDM.QuotedBodySnapshot != "dm nonce message" {
		t.Fatalf("expected direct quote snapshot, got %#v", quotedDM)
	}
	legacyDMRoute, err := st.ResolveLegacyRouteTarget(ctx, member.ID, workspace.ID, dm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyDMRoute.TargetType != "direct" || legacyDMRoute.TargetID != dm.ID {
		t.Fatalf("unexpected legacy DM route: %#v", legacyDMRoute)
	}

	results, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "searchable", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected workspace search to exclude DMs, got %#v", results)
	}
	scopedResults, err := st.SearchMessages(ctx, workspace.ID, channel.ID, owner.ID, "searchable", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopedResults) != 1 || scopedResults[0].Message.ID != publicMessage.ID {
		t.Fatalf("expected channel search to stay in channel, got %#v; other=%s", scopedResults, otherChannelMessage.ID)
	}
	quotedResults, err := st.SearchMessages(ctx, workspace.ID, channel.ID, owner.ID, "quotechannel", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotedResults) != 1 || quotedResults[0].Message.ID != quotedPublicMessage.ID || quotedResults[0].Message.QuotedBodySnapshot != "public searchable channel text" {
		t.Fatalf("expected quoted search result with snapshot, got %#v", quotedResults)
	}
	dmOnlyResults, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "secret", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dmOnlyResults) != 0 {
		t.Fatalf("expected DM-only search term to be hidden from workspace search, got %#v", dmOnlyResults)
	}

	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: dmMessage.ID, UserID: workspaceOnly.ID, Body: "blocked"}); err == nil {
		t.Fatal("expected non-DM member to be blocked from updating a DM message")
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: dmMessage.ID, UserID: workspaceOnly.ID}); err == nil {
		t.Fatal("expected non-DM member to be blocked from deleting a DM message")
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: dmMessage.ID, UserID: workspaceOnly.ID, Emoji: "nope"}); err == nil {
		t.Fatal("expected non-DM member to be blocked from reacting to a DM message")
	}
	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "dm.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/dm.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: dmMessage.ID, UploadID: upload.ID, UserID: workspaceOnly.ID}); err == nil {
		t.Fatal("expected non-DM member to be blocked from attaching to a DM message")
	}
	privateUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "private.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/private.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: publicMessage.ID, UploadID: privateUpload.ID, UserID: member.ID}); err == nil {
		t.Fatal("expected member to be blocked from attaching another user's private upload")
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: dmMessage.ID, UploadID: upload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, upload.ID, workspaceOnly.ID); err == nil {
		t.Fatal("expected non-DM member to be blocked from fetching a DM upload")
	}
	if _, err := st.GetUpload(ctx, upload.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetMessage(ctx, "msg_missing", owner.ID); err == nil {
		t.Fatal("expected missing message read to fail")
	}

	reply, state, events, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: dmMessage.ID,
		AuthorID:      member.ID,
		Body:          "dm thread reply secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.DirectConversationID != dm.ID || reply.ChannelID != "" || state.ReplyCount != 1 || len(events) != 2 {
		t.Fatalf("unexpected DM thread reply result: %#v %#v %#v", reply, state, events)
	}
	if _, _, _, err := st.GetThread(ctx, reply.ID, owner.ID, 10); err == nil {
		t.Fatal("expected nested thread root read to be blocked")
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: reply.ID,
		AuthorID:      owner.ID,
		Body:          "nested reply",
	}); err == nil {
		t.Fatal("expected nested thread reply to be blocked")
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: dmMessage.ID,
		AuthorID:      workspaceOnly.ID,
		Body:          "blocked reply",
	}); err == nil {
		t.Fatal("expected non-DM member thread reply to be blocked")
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: dmMessage.ID,
		AuthorID:      owner.ID,
		Body:          "bad nonce",
		Nonce:         strings.Repeat("x", 129),
	}); err == nil {
		t.Fatal("expected overlong thread nonce to be blocked")
	}
	page, err := st.ListDirectMessages(ctx, dm.ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	dmRootIDs := map[string]bool{}
	for _, msg := range page.Messages {
		dmRootIDs[msg.ID] = true
	}
	if len(page.Messages) != 3 || !dmRootIDs[dmMessage.ID] || !dmRootIDs[dmNonceMessage.ID] || !dmRootIDs[quotedDM.ID] || dmRootIDs[reply.ID] || page.NewestSeq != *quotedDM.ChannelSeq {
		t.Fatalf("expected DM root timeline to exclude thread replies, got %#v", page)
	}
	receipt, readEvent, err := st.MarkDirectRead(ctx, dm.ID, owner.ID, 999)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LastReadSeq != *quotedDM.ChannelSeq || len(readEvent.RecipientUserIDs) != 1 || readEvent.RecipientUserIDs[0] != owner.ID {
		t.Fatalf("expected DM read to cap to root sequence and target the reader, got %#v %#v", receipt, readEvent)
	}
	_, replies, _, err := st.GetThread(ctx, dmMessage.ID, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || replies[0].ID != reply.ID || replies[0].DirectConversationID != dm.ID {
		t.Fatalf("unexpected DM thread replies: %#v", replies)
	}
	if _, _, _, err := st.GetThread(ctx, dmMessage.ID, workspaceOnly.ID, 10); err == nil {
		t.Fatal("expected non-DM member to be blocked from DM thread")
	}

	ownerEvents, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	memberEvents, err := st.ListEventsAfter(ctx, workspace.ID, member.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	workspaceOnlyEvents, err := st.ListEventsAfter(ctx, workspace.ID, workspaceOnly.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, events := range [][]store.Event{ownerEvents, memberEvents} {
		if !hasEventForMessage(events, "message.created", dmMessage.ID) {
			t.Fatalf("expected DM member event replay to include DM message event: %#v", events)
		}
		if !hasEventForMessage(events, "thread.reply_created", reply.ID) {
			t.Fatalf("expected DM member event replay to include DM thread event: %#v", events)
		}
	}
	if hasEventForMessage(workspaceOnlyEvents, "message.created", dmMessage.ID) || hasEventForMessage(workspaceOnlyEvents, "thread.reply_created", reply.ID) {
		t.Fatalf("expected workspace-only member replay to hide DM events, got %#v", workspaceOnlyEvents)
	}
	for _, event := range workspaceOnlyEvents {
		if event.Type == "dm.read" || event.Type == "channel.read" {
			t.Fatalf("expected private read receipts to be filtered at replay query, got %#v", workspaceOnlyEvents)
		}
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM event_recipients WHERE event_id IN (SELECT id FROM events WHERE type = 'message.created' AND json_extract(payload_json, '$.direct_conversation_id') = ?)`, dm.ID); err != nil {
		t.Fatal(err)
	}
	workspaceOnlyEvents, err = st.ListEventsAfter(ctx, workspace.ID, workspaceOnly.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if hasEventForMessage(workspaceOnlyEvents, "message.created", dmMessage.ID) {
		t.Fatalf("expected private event flag to hide DM event even after recipient rows are removed, got %#v", workspaceOnlyEvents)
	}
}

func TestDeletedMessageAttachmentsAreHidden(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Attachment Member", Email: "attachment-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	channelMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "with upload"})
	if err != nil {
		t.Fatal(err)
	}
	channelUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "channel.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/channel.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: channelMessage.ID, UploadID: channelUpload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, channelUpload.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: channelMessage.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, channelUpload.ID, member.ID); err == nil {
		t.Fatal("expected deleted channel message attachment to stop exposing upload to other members")
	}
	if _, err := st.GetUpload(ctx, channelUpload.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	deletedChannelMessage, err := st.GetMessage(ctx, channelMessage.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedChannelMessage.Attachments) != 0 {
		t.Fatalf("expected deleted message attachments to be hidden, got %#v", deletedChannelMessage.Attachments)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: channelMessage.ID, UploadID: channelUpload.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected attaching to a deleted message to fail")
	}

	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dmMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "dm upload"})
	if err != nil {
		t.Fatal(err)
	}
	dmUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "dm.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/dm.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: dmMessage.ID, UploadID: dmUpload.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, dmUpload.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: dmMessage.ID, UserID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, dmUpload.ID, member.ID); err == nil {
		t.Fatal("expected deleted DM attachment to stop exposing upload to DM members")
	}
}

func TestMessagePrivacyScalingMessageShapeAndSequenceGuards(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES ('msg_invalid_shape', ?, ?, 'dm_fake', ?, NULL, 'msg_invalid_shape', 999, NULL, 'bad', 'markdown', ?)`,
		workspace.ID, channel.ID, owner.ID, now()); err == nil {
		t.Fatal("expected invalid message shape to be rejected")
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES ('msg_duplicate_seq', ?, ?, NULL, ?, NULL, 'msg_duplicate_seq', ?, NULL, 'dupe', 'markdown', ?)`,
		workspace.ID, channel.ID, owner.ID, *root.ChannelSeq, now()); err == nil {
		t.Fatal("expected duplicate channel sequence to be rejected")
	}
	otherRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "other root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES ('msg_mismatched_thread', ?, ?, NULL, ?, ?, ?, NULL, 1, 'bad reply', 'markdown', ?)`,
		workspace.ID, channel.ID, owner.ID, root.ID, otherRoot.ID, now()); err == nil {
		t.Fatal("expected mismatched parent and thread root to be rejected")
	}
	otherChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, UserID: owner.ID, Name: "wrong-thread-surface"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES ('msg_wrong_channel_thread', ?, ?, NULL, ?, ?, ?, NULL, 1, 'bad reply', 'markdown', ?)`,
		workspace.ID, otherChannel.ID, owner.ID, root.ID, root.ID, now()); err == nil {
		t.Fatal("expected reply on the wrong channel surface to be rejected")
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at)
		VALUES ('msg_valid_shape_reply', ?, ?, NULL, ?, ?, ?, NULL, 1, 'valid reply', 'markdown', ?)`,
		workspace.ID, channel.ID, owner.ID, root.ID, root.ID, now()); err != nil {
		t.Fatalf("expected valid root reply shape to be accepted: %v", err)
	}
}

func TestMessagePrivacyScalingChannelAndUploadVisibilityCoverage(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)

	gotChannel, err := st.GetChannel(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotChannel.ID != channel.ID || gotChannel.WorkspaceID != workspace.ID {
		t.Fatalf("unexpected channel: %#v", gotChannel)
	}
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "stranger-upload@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetChannel(ctx, channel.ID, stranger.ID); err == nil {
		t.Fatal("expected non-member channel read to be blocked")
	}
	if _, err := st.GetChannel(ctx, "chn_missing", owner.ID); err == nil {
		t.Fatal("expected missing channel read to fail")
	}
	if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected single-member direct conversation to be blocked")
	}
	if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{stranger.ID}}); err == nil {
		t.Fatal("expected direct conversation with non-workspace member to be blocked")
	}

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member-upload@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	ownerMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "owner upload message"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "bad nonce", Nonce: strings.Repeat("x", 129)}); err == nil {
		t.Fatal("expected overlong message nonce to be blocked")
	}
	if _, _, _, err := st.GetThread(ctx, "msg_missing", owner.ID, 10); err == nil {
		t.Fatal("expected missing thread read to fail")
	}
	memberMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: member.ID, Body: "member upload message"})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: owner.ID, Filename: "shared.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: ownerMessage.ID, UploadID: upload.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if firstEvent.ID == "" {
		t.Fatalf("expected first attachment to emit event, got %#v", firstEvent)
	}
	duplicateEvent, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: ownerMessage.ID, UploadID: upload.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if duplicateEvent.ID != "" {
		t.Fatalf("expected duplicate attachment to be a no-op, got event %#v", duplicateEvent)
	}
	if _, err := st.GetUpload(ctx, upload.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: ownerMessage.ID, UploadID: upload.ID, UserID: member.ID}); !errors.Is(err, store.ErrMessageNotWritable) {
		t.Fatalf("expected member to be blocked from attaching to another user's message, got %v", err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: memberMessage.ID, UploadID: upload.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected attaching to another user's message to be blocked")
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: memberMessage.ID, UploadID: upload.ID, UserID: member.ID}); err != nil {
		t.Fatal(err)
	}
	demotedUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: member.ID, Filename: "demoted.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/demoted.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: memberMessage.ID, UploadID: demotedUpload.ID, UserID: member.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUpload(ctx, demotedUpload.ID, member.ID); err == nil {
		t.Fatal("expected demoted upload owner to lose attached upload visibility")
	}

	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Upload Workspace"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherUpload, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: otherWorkspace.ID, OwnerID: owner.ID, Filename: "other.txt", ContentType: "text/plain", ByteSize: 1, StoragePath: "/tmp/other.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AttachUpload(ctx, store.AttachUploadInput{MessageID: ownerMessage.ID, UploadID: otherUpload.ID, UserID: owner.ID}); err == nil {
		t.Fatal("expected cross-workspace upload attach to be blocked")
	}
}

func TestMessagePrivacyScalingDirectMemberHydrationBatchesLargeInputs(t *testing.T) {
	t.Parallel()
	ctx, st, _, _, _ := seededStore(t)
	ids := make([]string, directConversationMemberHydrationBatchSize+25)
	for i := range ids {
		ids[i] = "dm_missing_" + strings.Repeat("x", i%8)
	}
	members, err := st.directConversationMembersByConversationIDs(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members for missing conversations, got %#v", members)
	}
}

func TestMessagePrivacyScalingMigrationBackfillsCurrentMainDMThreadPrivacy(t *testing.T) {
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrations(t, ctx, st, "0001_initial.sql", "0002_auth.sql", "0003_user_profiles.sql", "0004_message_quotes.sql", "0005_upload_dimensions.sql", "0006_message_client_nonce.sql", "0007_unread.sql", "0008_message_paging_indexes.sql", "0008_user_notification_settings.sql", "0009_bots.sql")

	const (
		workspaceID     = "wsp_migration"
		ownerID         = "usr_owner"
		memberID        = "usr_member"
		workspaceOnlyID = "usr_workspace_only"
		dmID            = "dm_private"
		rootID          = "msg_dm_root"
		replyID         = "msg_dm_reply"
	)
	if _, err := st.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES (?, 'Owner', '', '2026-01-01T00:00:00Z')`, ownerID)
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES (?, 'Member', '', '2026-01-01T00:00:00Z')`, memberID)
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES (?, 'Workspace Only', '', '2026-01-01T00:00:00Z')`, workspaceOnlyID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspaces (id, name, slug, created_at) VALUES (?, 'Workspace', 'workspace', '2026-01-01T00:00:00Z')`, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'owner', '2026-01-01T00:00:00Z')`, workspaceID, ownerID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'member', '2026-01-01T00:00:00Z')`, workspaceID, memberID)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (?, ?, 'member', '2026-01-01T00:00:00Z')`, workspaceID, workspaceOnlyID)
	mustExecSQL(t, ctx, st, `INSERT INTO direct_conversations (id, workspace_id, created_at) VALUES (?, ?, '2026-01-01T00:00:00Z')`, dmID, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO direct_conversation_members (conversation_id, user_id, created_at) VALUES (?, ?, '2026-01-01T00:00:00Z')`, dmID, ownerID)
	mustExecSQL(t, ctx, st, `INSERT INTO direct_conversation_members (conversation_id, user_id, created_at) VALUES (?, ?, '2026-01-01T00:00:00Z')`, dmID, memberID)
	mustExecSQL(t, ctx, st, `INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at) VALUES (?, ?, NULL, ?, ?, NULL, ?, 1, NULL, 'private root', 'markdown', '2026-01-01T00:00:01Z')`, rootID, workspaceID, dmID, ownerID, rootID)
	mustExecSQL(t, ctx, st, `INSERT INTO thread_state (root_message_id, reply_count, last_reply_at, last_reply_author_ids_json) VALUES (?, 1, '2026-01-01T00:00:02Z', '["usr_member"]')`, rootID)
	mustExecSQL(t, ctx, st, `INSERT INTO messages (id, workspace_id, channel_id, direct_conversation_id, author_id, parent_message_id, thread_root_id, channel_seq, thread_seq, body, body_format, created_at) VALUES (?, ?, '', NULL, ?, ?, ?, NULL, 1, 'private reply', 'markdown', '2026-01-01T00:00:02Z')`, replyID, workspaceID, memberID, rootID, rootID)
	mustExecSQL(t, ctx, st, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at) VALUES ('evt_dm_message', 'cur_001', ?, NULL, 'message.created', 1, '{"message_id":"msg_dm_root","direct_conversation_id":"dm_private","author_id":"usr_owner"}', '2026-01-01T00:00:01Z')`, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at) VALUES ('evt_thread_reply', 'cur_002', ?, NULL, 'thread.reply_created', NULL, '{"message_id":"msg_dm_reply","root_message_id":"msg_dm_root"}', '2026-01-01T00:00:02Z')`, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at) VALUES ('evt_thread_state', 'cur_003', ?, NULL, 'thread.state_updated', NULL, '{"root_message_id":"msg_dm_root"}', '2026-01-01T00:00:02Z')`, workspaceID)
	mustExecSQL(t, ctx, st, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at) VALUES ('evt_reply_reaction', 'cur_004', ?, NULL, 'reaction.added', NULL, '{"message_id":"msg_dm_reply","emoji":":ok:"}', '2026-01-01T00:00:03Z')`, workspaceID)
	if _, err := st.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}

	applySQLiteMigrations(t, ctx, st, "0010_message_privacy_scaling.sql")

	var replyDM string
	var replyChannel any
	if err := st.db.QueryRowContext(ctx, `SELECT direct_conversation_id, channel_id FROM messages WHERE id = ?`, replyID).Scan(&replyDM, &replyChannel); err != nil {
		t.Fatal(err)
	}
	if replyDM != dmID || replyChannel != nil {
		t.Fatalf("expected migration to attach old DM reply to DM surface, got dm=%q channel=%#v", replyDM, replyChannel)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE id IN ('evt_dm_message', 'evt_thread_reply', 'evt_thread_state', 'evt_reply_reaction') AND is_private = 1`); got != 4 {
		t.Fatalf("expected all historical DM events to be private, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM event_recipients WHERE event_id IN ('evt_thread_reply', 'evt_thread_state', 'evt_reply_reaction')`); got != 6 {
		t.Fatalf("expected historical DM thread/reaction events to be recipient-scoped, got %d", got)
	}
	workspaceOnlyEvents, err := st.ListEventsAfter(ctx, workspaceID, workspaceOnlyID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaceOnlyEvents) != 0 {
		t.Fatalf("expected workspace-only member to see no private historical DM events, got %#v", workspaceOnlyEvents)
	}
}

func TestDefaultWorkspaceOwnerMigrationSkipsGuests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	applySQLiteMigrations(t, ctx, st, "0001_initial.sql")
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES ('usr_default', 'Default', '', '2026-01-01T00:00:00Z')`)
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, avatar_url, created_at) VALUES ('usr_guest', 'Guest', '', '2026-01-01T00:00:00Z')`)
	mustExecSQL(t, ctx, st, `INSERT INTO workspaces (id, name, slug, created_at) VALUES ('wsp_default', 'ClickClack', 'clickclack', '2026-01-01T00:00:00Z')`)
	mustExecSQL(t, ctx, st, `INSERT INTO workspaces (id, name, slug, created_at) VALUES ('wsp_guests', 'Guests', 'guests', '2026-01-01T00:00:00Z')`)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES ('wsp_default', 'usr_default', 'member', '2026-01-01T00:00:00Z')`)
	mustExecSQL(t, ctx, st, `INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES ('wsp_guests', 'usr_guest', 'member', '2026-01-01T00:00:00Z')`)

	applySQLiteMigrations(t, ctx, st, "0013_default_workspace_owner.sql")
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id = 'wsp_default' AND user_id = 'usr_default' AND role = 'owner'`); got != 1 {
		t.Fatalf("expected default workspace member to become owner, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id = 'wsp_guests' AND user_id = 'usr_guest' AND role = 'owner'`); got != 0 {
		t.Fatalf("guest workspace member should not become owner, got %d", got)
	}
}

func TestMessagePrivacyScalingHotPathQueryPlans(t *testing.T) {
	t.Parallel()
	ctx, st, _, _, _ := seededStore(t)
	for _, tc := range []struct {
		name      string
		query     string
		args      []any
		wantIndex string
	}{
		{
			name:      "channel page",
			query:     `EXPLAIN QUERY PLAN SELECT m.id FROM messages m WHERE m.channel_id = ? AND m.parent_message_id IS NULL ORDER BY m.channel_seq DESC LIMIT ?`,
			args:      []any{"chn_x", 100},
			wantIndex: "idx_messages_channel_root_page",
		},
		{
			name:      "dm page",
			query:     `EXPLAIN QUERY PLAN SELECT m.id FROM messages m WHERE m.direct_conversation_id = ? AND m.parent_message_id IS NULL ORDER BY m.channel_seq DESC LIMIT ?`,
			args:      []any{"dm_x", 100},
			wantIndex: "idx_messages_direct_page",
		},
		{
			name:      "dm sidebar membership",
			query:     `EXPLAIN QUERY PLAN SELECT dc.id FROM direct_conversation_members dcm JOIN direct_conversations dc ON dc.id = dcm.conversation_id WHERE dcm.user_id = ? ORDER BY dc.created_at`,
			args:      []any{"usr_x"},
			wantIndex: "idx_direct_conversation_members_user",
		},
		{
			name:      "bot historical workspaces",
			query:     `EXPLAIN QUERY PLAN SELECT m.workspace_id FROM messages m WHERE m.author_id = ?`,
			args:      []any{"usr_x"},
			wantIndex: "idx_messages_author_workspace",
		},
		{
			name:      "attachment hydration",
			query:     `EXPLAIN QUERY PLAN SELECT u.id FROM message_attachments ma JOIN uploads u ON u.id = ma.upload_id WHERE ma.message_id = ? ORDER BY ma.created_at`,
			args:      []any{"msg_x"},
			wantIndex: "idx_message_attachments_message_created",
		},
		{
			name:      "upload attachment visibility",
			query:     `EXPLAIN QUERY PLAN SELECT ma.message_id FROM message_attachments ma WHERE ma.upload_id = ? LIMIT 1`,
			args:      []any{"upl_x"},
			wantIndex: "idx_message_attachments_upload_message",
		},
		{
			name: "event replay",
			query: `EXPLAIN QUERY PLAN
					SELECT e.id
					FROM events e
					WHERE e.workspace_id = ? AND e.cursor > ?
					  AND (
					    e.is_private = 0
					    OR EXISTS (SELECT 1 FROM event_recipients er WHERE er.event_id = e.id AND er.user_id = ?)
					  )
					ORDER BY e.cursor
				LIMIT ?`,
			args:      []any{"wsp_x", "cur_x", "usr_x", 200},
			wantIndex: "idx_events_workspace_cursor",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := explainQueryPlan(t, ctx, st, tc.query, tc.args...)
			if !strings.Contains(plan, tc.wantIndex) {
				t.Fatalf("expected query plan to use %s, got:\n%s", tc.wantIndex, plan)
			}
		})
	}
}

func TestMessagePrivacyScalingEventPruning(t *testing.T) {
	t.Parallel()
	ctx, st, owner, workspace, channel := seededStore(t)
	message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "event prune message"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkChannelRead(ctx, channel.ID, owner.ID, *message.ChannelSeq); err != nil {
		t.Fatal(err)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM event_recipients`); got == 0 {
		t.Fatal("expected private read event recipients before pruning")
	}
	if _, err := st.PruneEvents(ctx, workspace.ID, 0, ""); err == nil {
		t.Fatal("expected prune without retention bounds to be rejected")
	}
	if _, err := st.PruneEvents(ctx, "", 1, ""); err == nil {
		t.Fatal("expected prune without workspace to be rejected")
	}
	if _, err := st.PruneEvents(ctx, workspace.ID, -1, ""); err == nil {
		t.Fatal("expected prune with negative keep_latest to be rejected")
	}
	if _, err := st.PruneEvents(ctx, workspace.ID, 0, "not-a-time"); err == nil {
		t.Fatal("expected prune with invalid cutoff to be rejected")
	}
	pruned, err := st.PruneEvents(ctx, workspace.ID, 0, "9999-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if pruned == 0 {
		t.Fatal("expected pruning to delete events")
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE workspace_id = ?`, workspace.ID); got != 0 {
		t.Fatalf("expected all workspace events to be pruned, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM event_recipients`); got != 0 {
		t.Fatalf("expected event recipient rows to cascade on prune, got %d", got)
	}

	for i := 0; i < 3; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "event keep latest"}); err != nil {
			t.Fatal(err)
		}
	}
	pruned, err = st.PruneEvents(ctx, workspace.ID, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 2 {
		t.Fatalf("expected keep_latest prune to delete 2 events, got %d", pruned)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE workspace_id = ?`, workspace.ID); got != 1 {
		t.Fatalf("expected latest event to be retained, got %d", got)
	}
}

func TestMessagePrivacyScalingEventPruningUsesTimestampCutoff(t *testing.T) {
	t.Parallel()
	ctx, st, _, workspace, _ := seededStore(t)
	mustExecSQL(t, ctx, st, `DELETE FROM event_recipients`)
	mustExecSQL(t, ctx, st, `DELETE FROM events WHERE workspace_id = ?`, workspace.ID)
	mustExecSQL(t, ctx, st, `
		INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at, is_private)
		VALUES
		  ('evt_before_cutoff', 'cur_prune_001', ?, NULL, 'test.before', NULL, '{}', '2025-12-31T23:59:59.999999999Z', 0),
		  ('evt_at_cutoff', 'cur_prune_002', ?, NULL, 'test.equal', NULL, '{}', '2026-01-01T00:00:00Z', 0),
		  ('evt_after_fractional_cutoff', 'cur_prune_003', ?, NULL, 'test.after', NULL, '{}', '2026-01-01T00:00:00.5Z', 0)
	`, workspace.ID, workspace.ID, workspace.ID)

	pruned, err := st.PruneEvents(ctx, workspace.ID, 0, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("expected only event before cutoff to be pruned, got %d", pruned)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE id = 'evt_before_cutoff'`); got != 0 {
		t.Fatalf("expected event before cutoff to be deleted, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE id = 'evt_at_cutoff'`); got != 1 {
		t.Fatalf("expected event exactly at cutoff to be retained, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM events WHERE id = 'evt_after_fractional_cutoff'`); got != 1 {
		t.Fatalf("expected fractional event after cutoff to be retained, got %d", got)
	}
}

func hasEventForMessage(events []store.Event, eventType, messageID string) bool {
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		switch payload := event.Payload.(type) {
		case map[string]string:
			if payload["message_id"] == messageID {
				return true
			}
		case map[string]any:
			if got, _ := payload["message_id"].(string); got == messageID {
				return true
			}
		}
	}
	return false
}

func scalarCount(t *testing.T, ctx context.Context, st *Store, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := st.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func explainQueryPlan(t *testing.T, ctx context.Context, st *Store, query string, args ...any) string {
	t.Helper()
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, "\n")
}

func applySQLiteMigrations(t *testing.T, ctx context.Context, st *Store, names ...string) {
	t.Helper()
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func mustExecSQL(t *testing.T, ctx context.Context, st *Store, query string, args ...any) {
	t.Helper()
	if _, err := st.db.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
