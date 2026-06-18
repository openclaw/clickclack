package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestGuestWaitingRoomModeration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	bystander, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Bystander", Email: "bystander@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, bystander.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	peerModerator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Peer Moderator", Email: "peer-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, peerModerator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	adminBot, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Admin Bot", Scopes: []string{"bot:admin"}, CreatedBy: moderator.ID})
	if err != nil {
		t.Fatal(err)
	}

	guestChannels, err := st.ListChannels(ctx, workspace.ID, guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(guestChannels) != 1 || guestChannels[0].Name != "guest" {
		t.Fatalf("guest should only see #guest, got %#v", guestChannels)
	}
	moderatorChannels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(moderatorChannels) < 2 {
		t.Fatalf("moderator should see waiting and approved rooms, got %#v", moderatorChannels)
	}
	var guestChannelID, generalChannelID string
	for _, channel := range moderatorChannels {
		switch channel.Name {
		case "guest":
			guestChannelID = channel.ID
		case "general":
			generalChannelID = channel.ID
		}
	}
	if guestChannelID == "" || generalChannelID == "" {
		t.Fatalf("expected guest and general channels, got %#v", moderatorChannels)
	}

	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: guest.ID, Body: "let me in"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected guest to be blocked from #general, got %v", err)
	}
	if _, _, err := st.MarkChannelRead(ctx, generalChannelID, guest.ID, 1); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected guest to be blocked from hidden channel reads, got %v", err)
	}
	beforeHidden, err := st.ListEventsAfter(ctx, workspace.ID, guest.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	cursorBeforeHidden := lastEventCursor(beforeHidden)
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: moderator.ID, Body: "hidden event"}); err != nil {
		t.Fatal(err)
	}
	var firstGuestMessage store.Message
	for i := 0; i < store.GuestPostLimit; i++ {
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: guest.ID, Body: "waiting"})
		if err != nil {
			t.Fatalf("guest post %d failed: %v", i+1, err)
		}
		if i == 0 {
			firstGuestMessage = message
		}
	}
	if _, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: firstGuestMessage.ID, UserID: guest.ID}); err != nil {
		t.Fatalf("guest should be able to delete own visible waiting-room post, got %v", err)
	}
	visibleEvents, err := st.ListEventsAfter(ctx, workspace.ID, guest.ID, cursorBeforeHidden, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(visibleEvents) != 1 || visibleEvents[0].ChannelID != guestChannelID {
		t.Fatalf("expected event page limit to apply after access filtering, got %#v", visibleEvents)
	}
	if _, _, err := st.MarkChannelRead(ctx, guestChannelID, guest.ID, 1); err != nil {
		t.Fatalf("expected guest channel read to succeed, got %v", err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: guest.ID, Body: "too much"}); !errors.Is(err, store.ErrPostRateLimited) {
		t.Fatalf("expected guest post limit, got %v", err)
	}

	members, err := st.ListWorkspaceMembers(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestModeration store.MemberModeration
	for _, member := range members {
		if member.User.ID == guest.ID {
			guestModeration = member
		}
	}
	if guestModeration.Role != store.WorkspaceRoleGuest || guestModeration.PostsRemaining != 0 || guestModeration.PostLimit != store.GuestPostLimit {
		t.Fatalf("unexpected guest moderation state: %#v", guestModeration)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: guest.ID, TargetUserID: moderator.ID, Role: store.WorkspaceRoleGuest}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected guest moderation denial, got %v", err)
	}
	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: peerModerator.ID, Blocked: &blocked}); err == nil {
		t.Fatal("expected peer moderator moderation to fail")
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: adminBot.ID, Blocked: &blocked}); err == nil {
		t.Fatal("expected bot moderation by moderator to fail")
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, Role: "blocked"}); err == nil {
		t.Fatal("expected invalid moderation role to fail")
	}
	_, approvalEvent, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, Role: store.WorkspaceRoleMember, ClearTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(approvalEvent.RecipientUserIDs) == 0 {
		t.Fatalf("expected recipient-scoped moderation event, got %#v", approvalEvent)
	}
	bystanderEvents, err := st.ListEventsAfter(ctx, workspace.ID, bystander.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range bystanderEvents {
		if event.ID == approvalEvent.ID {
			t.Fatalf("moderation event leaked to unrelated guest: %#v", event)
		}
	}
	targetEvents, err := st.ListEventsAfter(ctx, workspace.ID, guest.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !eventListContains(targetEvents, approvalEvent.ID) {
		t.Fatalf("target did not receive moderation event: %#v", targetEvents)
	}
	guestChannels, err = st.ListChannels(ctx, workspace.ID, guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(guestChannels) < 2 {
		t.Fatalf("approved guest should see more rooms, got %#v", guestChannels)
	}
	var approvedMessage store.Message
	for i := 0; i < 2; i++ {
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: guest.ID, Body: "approved"})
		if err != nil {
			t.Fatalf("approved post %d failed: %v", i+1, err)
		}
		if i == 0 {
			approvedMessage = message
		}
	}

	timeoutUntil := time.Now().Add(time.Hour).In(time.FixedZone("plus-one", 3600)).Format(time.RFC3339)
	note := "watch carefully"
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, TimeoutUntil: &timeoutUntil, ModerationNote: &note}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: guest.ID, Body: "timed out"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected timeout to block posts, got %v", err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, ClearTimeout: true, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	members, err = st.ListWorkspaceMembers(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.User.ID == guest.ID && member.ModerationNote != note {
			t.Fatalf("partial moderation update cleared note: %#v", member)
		}
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: guest.ID, Body: "blocked"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop posts, got %v", err)
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{MessageID: approvedMessage.ID, UserID: guest.ID, Body: "blocked edit"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop edits, got %v", err)
	}
	if _, err := st.AddReaction(ctx, store.CreateReactionInput{MessageID: approvedMessage.ID, UserID: guest.ID, Emoji: "+1"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop reactions, got %v", err)
	}
	if _, _, err := st.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: workspace.ID, Name: "escape", Kind: "public", UserID: guest.ID}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop channel creation, got %v", err)
	}
	if _, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: generalChannelID, UserID: guest.ID, Name: "renamed"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop channel updates, got %v", err)
	}
	if _, err := st.CreateUpload(ctx, store.CreateUploadInput{WorkspaceID: workspace.ID, OwnerID: guest.ID, Filename: "blocked.txt", ContentType: "text/plain", ByteSize: 7, StoragePath: "blocked.txt"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected block to stop upload creation, got %v", err)
	}
	blocked = false
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, ClearTimeout: true, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: guest.ID, Body: "back"}); err != nil {
		t.Fatalf("expected unblock to restore posting, got %v", err)
	}
}

func eventListContains(events []store.Event, eventID string) bool {
	for _, event := range events {
		if event.ID == eventID {
			return true
		}
	}
	return false
}

func lastEventCursor(events []store.Event) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Cursor
}

func TestGuestWorkspaceRoleSyncRevokesModeratorOrgRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Mod", Email: "revoked-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, user.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Role != store.WorkspaceRoleModerator {
		t.Fatalf("expected moderator role, got %#v", workspace)
	}
	workspace, err = st.EnsureDefaultGuestWorkspaceMember(ctx, user.ID, store.WorkspaceRoleGuest)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Role != store.WorkspaceRoleGuest {
		t.Fatalf("expected revoked moderator to become guest, got %#v", workspace)
	}

	workspace, err = st.EnsureDefaultGuestWorkspaceMember(ctx, user.ID, store.WorkspaceRoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Role != store.WorkspaceRoleMember {
		t.Fatalf("expected approved member role, got %#v", workspace)
	}
	workspace, err = st.EnsureDefaultGuestWorkspaceMember(ctx, user.ID, store.WorkspaceRoleGuest)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Role != store.WorkspaceRoleMember {
		t.Fatalf("expected approved member to remain member, got %#v", workspace)
	}
}

func TestBlockedModeratorCannotModerate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Owner", Email: "owner-block-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, owner.ID, store.WorkspaceRoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "blocked-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	timedModerator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Timed Moderator", Email: "timed-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, timedModerator.ID, store.WorkspaceRoleModerator); err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "blocked-mod-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}

	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: moderator.ID, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ListWorkspaceMembers(ctx, workspace.ID, moderator.ID); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected blocked moderator to lose roster access, got %v", err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: guest.ID, Role: store.WorkspaceRoleMember}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected blocked moderator to lose moderation powers, got %v", err)
	}
	blockEventNote := "blocked moderator should not see this"
	_, blockEvent, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: guest.ID, Role: store.WorkspaceRoleMember, ModerationNote: &blockEventNote})
	if err != nil {
		t.Fatal(err)
	}
	blockedModeratorEvents, err := st.ListEventsAfter(ctx, workspace.ID, moderator.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if eventListContains(blockedModeratorEvents, blockEvent.ID) {
		t.Fatalf("blocked moderator received private moderation event: %#v", blockedModeratorEvents)
	}
	timeoutUntil := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: timedModerator.ID, TimeoutUntil: &timeoutUntil}); err != nil {
		t.Fatal(err)
	}
	timeoutEventNote := "timed moderator should not see this"
	_, timeoutEvent, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: guest.ID, ModerationNote: &timeoutEventNote})
	if err != nil {
		t.Fatal(err)
	}
	timedModeratorEvents, err := st.ListEventsAfter(ctx, workspace.ID, timedModerator.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if eventListContains(timedModeratorEvents, timeoutEvent.ID) {
		t.Fatalf("timed-out moderator received private moderation event: %#v", timedModeratorEvents)
	}
}

func TestGuestPostBudgetIgnoresPreDemotionMemberPosts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "budget-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "budget-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestChannelID, generalChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "guest":
			guestChannelID = channel.ID
		case "general":
			generalChannelID = channel.ID
		}
	}
	if guestChannelID == "" || generalChannelID == "" {
		t.Fatalf("expected guest and general channels, got %#v", channels)
	}
	for i := 0; i < store.GuestPostLimit; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: member.ID, Body: "before demotion"}); err != nil {
			t.Fatalf("member post %d failed: %v", i+1, err)
		}
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: member.ID, Body: "first waiting-room post"}); err != nil {
		t.Fatalf("pre-demotion member posts should not consume guest budget, got %v", err)
	}
}

func TestDemotedGuestCannotReplayHiddenChannelNonce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "nonce-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "nonce-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generalChannelID string
	for _, channel := range channels {
		if channel.Name == "general" {
			generalChannelID = channel.ID
		}
	}
	if generalChannelID == "" {
		t.Fatalf("expected general channel, got %#v", channels)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: member.ID, Body: "before demotion", Nonce: "nonce-replay"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: member.ID, Body: "before demotion", Nonce: "nonce-replay"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected replayed hidden channel nonce to be blocked, got %v", err)
	}
}

func TestDemotedGuestCannotUseExistingDirectConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "dm-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "dm-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: moderator.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: moderator.ID, Body: "before demotion"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	mustExecSQL(t, ctx, st, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at, is_private) VALUES ('evt_legacy_dm_message', 'cur_legacy_dm_message', ?, NULL, 'message.updated', NULL, ?, '2026-01-01T00:00:00Z', 1)`, workspace.ID, `{"message_id":"`+message.ID+`","author_id":"`+moderator.ID+`"}`)
	mustExecSQL(t, ctx, st, `INSERT INTO event_recipients (event_id, user_id) VALUES ('evt_legacy_dm_message', ?)`, member.ID)
	if _, err := st.ResolveRouteTarget(ctx, member.ID, workspace.RouteID, dm.RouteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected demoted guest direct route to be hidden, got %v", err)
	}
	if _, err := st.GetMessage(ctx, message.ID, member.ID); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected demoted guest to be blocked from DM message, got %v", err)
	}
	if _, err := st.ListDirectMessages(ctx, dm.ID, member.ID, store.MessagePageRequest{}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected demoted guest to be blocked from DM history, got %v", err)
	}
	if _, _, err := st.MarkDirectRead(ctx, dm.ID, member.ID, 1); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected demoted guest to be blocked from DM read receipts, got %v", err)
	}
	events, err := st.ListEventsAfter(ctx, workspace.ID, member.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		payload, _ := event.Payload.(map[string]any)
		if payload["direct_conversation_id"] == dm.ID || event.ID == "evt_legacy_dm_message" {
			t.Fatalf("direct event leaked to demoted guest: %#v", event)
		}
	}
}

func TestGuestSearchOnlyReturnsWaitingRoomMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	moderator, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Moderator", Email: "search-mod@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultGuestWorkspaceMember(ctx, moderator.ID, store.WorkspaceRoleModerator)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Guest", Email: "search-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureDefaultGuestWorkspaceMember(ctx, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, moderator.ID)
	if err != nil {
		t.Fatal(err)
	}
	var guestChannelID, generalChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "guest":
			guestChannelID = channel.ID
		case "general":
			generalChannelID = channel.ID
		}
	}
	if guestChannelID == "" || generalChannelID == "" {
		t.Fatalf("expected guest and general channels, got %#v", channels)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: moderator.ID, Body: "needle hidden"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: guest.ID, Body: "needle public"}); err != nil {
		t.Fatal(err)
	}

	results, err := st.SearchMessages(ctx, workspace.ID, "", guest.ID, "needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Message.ChannelID != guestChannelID {
		t.Fatalf("guest search leaked hidden messages: %#v", results)
	}
	if _, err := st.SearchMessages(ctx, workspace.ID, generalChannelID, guest.ID, "needle", 10); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected explicit hidden channel search to fail, got %v", err)
	}
}
