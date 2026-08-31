package sqlite

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestMentionsNotificationMigrationUpgradesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "mentions-upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0039_mentions_and_notifications.sql")

	owner, err := st.EnsureBootstrap(ctx, "Upgrade Owner", "upgrade-owner@example.com")
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
	if _, err := st.db.ExecContext(ctx, `INSERT INTO events (id, cursor, workspace_id, channel_id, type, seq, payload_json, created_at, is_private) VALUES ('evt_upgrade', 'cur_upgrade', ?, ?, 'message.created', 1, '{}', ?, 0)`, workspaces[0].ID, channels[0].ID, now()); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var mentionedJSON string
	if err := st.db.QueryRowContext(ctx, `SELECT mentioned_user_ids FROM events WHERE id = 'evt_upgrade'`).Scan(&mentionedJSON); err != nil {
		t.Fatal(err)
	}
	if mentionedJSON != "[]" {
		t.Fatalf("expected existing event to receive an empty mention list, got %q", mentionedJSON)
	}
	preference, err := st.GetChannelNotificationPreference(ctx, channels[0].ID, owner.ID)
	if err != nil || preference != store.ChannelNotifyAll {
		t.Fatalf("expected upgraded database to default to all, got %q: %v", preference, err)
	}
}

func TestParseMessageMentions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		expected []string
	}{
		{
			name:     "no mentions",
			body:     "hello world",
			expected: nil,
		},
		{
			name:     "single mention",
			body:     "hello @alice",
			expected: []string{"alice"},
		},
		{
			name:     "multiple mentions",
			body:     "hey @alice and @bob check this out",
			expected: []string{"alice", "bob"},
		},
		{
			name:     "mention at start",
			body:     "@alice look at this",
			expected: []string{"alice"},
		},
		{
			name:     "duplicate mentions",
			body:     "@alice hey @alice",
			expected: []string{"alice"},
		},
		{
			name:     "mention with underscore",
			body:     "hello @alice_smith",
			expected: []string{"alice_smith"},
		},
		{
			name:     "sentence-ending period",
			body:     "hello @alice.",
			expected: []string{"alice"},
		},
		{
			name:     "mention after punctuation",
			body:     `cc:"@alice" and cc:@bob`,
			expected: []string{"alice", "bob"},
		},
		{
			name:     "email not a mention",
			body:     "email me at user@example.com",
			expected: nil,
		},
		{
			name:     "bare URL path not a mention",
			body:     "see https://example.com/@alice then @bob",
			expected: []string{"bob"},
		},
		{
			name:     "mixed-case URL scheme path not a mention",
			body:     "see HTTPS://example.com/@alice then @bob",
			expected: []string{"bob"},
		},
		{
			name:     "www URL path not a mention",
			body:     "see www.example.com/@alice then @bob",
			expected: []string{"bob"},
		},
		{
			name:     "non-http URL scheme path not a mention",
			body:     "see ftp://example.com/@alice then @bob",
			expected: []string{"bob"},
		},
		{
			name:     "mention in markdown brackets not detected",
			body:     "check out [ask @user](https://example.com/@other)",
			expected: nil,
		},
		{
			name:     "ordinary brackets before a link do not hide mentions",
			body:     "[x] notify @alice [details](https://example.com)",
			expected: []string{"alice"},
		},
		{
			name:     "empty body",
			body:     "",
			expected: nil,
		},
		{
			name:     "just an @ symbol with no handle",
			body:     "just an @",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := store.ParseMessageMentions(tt.body)
			if len(result) == 0 && len(tt.expected) == 0 {
				return
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, result)
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Fatalf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestMentionRecordingOnMessageCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	// Create a user with a known handle.
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Set the handle since CreateUser sets it to empty.
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      user.ID,
		DisplayName: "Alice",
		Handle:      "alice",
	}); err != nil {
		t.Fatal(err)
	}

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = st.AddWorkspaceMember(ctx, workspace.ID, user.ID, store.WorkspaceRoleMember)
	if err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}
	channel := channels[0]

	// Create a message with an @mention.
	_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "hey @alice check this out",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify the event was created (mention recording happens inside the tx).
	events, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, evt := range events {
		if evt.ID == event.ID {
			found = true
			if len(evt.MentionedUserIDs) != 1 || evt.MentionedUserIDs[0] != user.ID {
				t.Fatalf("expected mentioned_user_ids to contain %q, got %v", user.ID, evt.MentionedUserIDs)
			}
			break
		}
	}
	if !found {
		t.Fatalf("event %s not found in event stream", event.ID)
	}

	// Create a message without mentions and verify no mentioned_user_ids.
	_, event2, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "no mentions here",
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err = st.ListEventsAfter(ctx, workspace.ID, owner.ID, event.Cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range events {
		if evt.ID == event2.ID {
			if len(evt.MentionedUserIDs) != 0 {
				t.Fatalf("expected empty mentioned_user_ids, got %v", evt.MentionedUserIDs)
			}
			break
		}
	}
}

func TestMentionRecordingOnThreadReply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      user.ID,
		DisplayName: "Alice",
		Handle:      "alice",
	}); err != nil {
		t.Fatal(err)
	}

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = st.AddWorkspaceMember(ctx, workspace.ID, user.ID, store.WorkspaceRoleMember)
	if err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}
	channel := channels[0]

	rootMsg, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "root message",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a thread reply with an @mention.
	_, _, events, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: rootMsg.ID,
		AuthorID:      owner.ID,
		Body:          "reply to @alice",
	})
	if err != nil {
		t.Fatal(err)
	}

	events2, err := st.ListEventsAfter(ctx, workspace.ID, owner.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}

	for _, evt := range events {
		if evt.Type != "thread.reply_created" {
			continue
		}
		for _, evt2 := range events2 {
			if evt.ID == evt2.ID {
				if len(evt2.MentionedUserIDs) != 1 || evt2.MentionedUserIDs[0] != user.ID {
					t.Fatalf("expected mentioned_user_ids for thread reply to contain %q, got %v", user.ID, evt2.MentionedUserIDs)
				}
				assertEventPayloadMissing(t, evt2, "author_id")
				assertEventPayloadMissing(t, evt2, "body")
			}
		}
	}
}

func TestMentionLookupUsesOneSQLiteParameter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "many-mentions-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Alice", Email: "many-mentions-alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	alice, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID: alice.ID, DisplayName: alice.DisplayName, Handle: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, alice.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil || len(channels) == 0 {
		t.Fatalf("expected a channel: %v", err)
	}
	var body strings.Builder
	body.WriteString("@alice ")
	for i := 0; i < 40_000; i++ {
		body.WriteString("@person")
		body.WriteString(strconv.Itoa(i))
		body.WriteByte(' ')
	}
	_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channels[0].ID,
		AuthorID:  owner.ID,
		Body:      body.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(event.MentionedUserIDs) != 1 || event.MentionedUserIDs[0] != alice.ID {
		t.Fatalf("expected only Alice to resolve from the large mention set, got %v", event.MentionedUserIDs)
	}
}

func TestDirectThreadMentionDoesNotGrantConversationAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "dm-mention-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "dm-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "dm-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID: outsider.ID, DisplayName: outsider.DisplayName, Handle: "outsider",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{member.ID, outsider.ID} {
		if err := st.AddWorkspaceMember(ctx, workspace.ID, userID, store.WorkspaceRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: direct.ID, AuthorID: owner.ID, Body: "private root",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, events, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: root.ID, AuthorID: owner.ID, Body: "private hello @outsider",
	})
	if err != nil {
		t.Fatal(err)
	}
	var replyEventID string
	for _, event := range events {
		if event.Type == "thread.reply_created" {
			replyEventID = event.ID
		}
	}
	if replyEventID == "" {
		t.Fatal("expected thread.reply_created event")
	}
	outsiderEvents, err := st.ListEventsAfter(ctx, workspace.ID, outsider.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range outsiderEvents {
		if event.ID == replyEventID {
			t.Fatal("mention must not grant an outsider access to a private conversation event")
		}
	}
	memberEvents, err := st.ListEventsAfter(ctx, workspace.ID, member.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range memberEvents {
		if event.ID == replyEventID {
			return
		}
	}
	t.Fatal("expected direct conversation member to receive the private thread event")
}

func TestChannelNotificationSettingsCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}
	channel := channels[0]

	// Default preference should be "all".
	pref, err := st.GetChannelNotificationPreference(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref != store.ChannelNotifyAll {
		t.Fatalf("expected default preference 'all', got %q", pref)
	}

	// Set to "mentions".
	err = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channel.ID,
		UserID:     owner.ID,
		Preference: store.ChannelNotifyMentions,
	})
	if err != nil {
		t.Fatal(err)
	}
	pref, err = st.GetChannelNotificationPreference(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref != store.ChannelNotifyMentions {
		t.Fatalf("expected 'mentions', got %q", pref)
	}

	// Set to "muted".
	err = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channel.ID,
		UserID:     owner.ID,
		Preference: store.ChannelNotifyMuted,
	})
	if err != nil {
		t.Fatal(err)
	}
	pref, err = st.GetChannelNotificationPreference(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref != store.ChannelNotifyMuted {
		t.Fatalf("expected 'muted', got %q", pref)
	}

	// Set back to "all".
	err = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channel.ID,
		UserID:     owner.ID,
		Preference: store.ChannelNotifyAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	pref, err = st.GetChannelNotificationPreference(ctx, channel.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref != store.ChannelNotifyAll {
		t.Fatalf("expected 'all', got %q", pref)
	}

	// Invalid preference should fail.
	err = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channel.ID,
		UserID:     owner.ID,
		Preference: "invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid preference")
	}
}

func TestPushNotificationRecipientsRespectChannelMute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	member, err = st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID: member.ID, DisplayName: member.DisplayName, Handle: "member",
	})
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember)
	if err != nil {
		t.Fatal(err)
	}

	// Enable push notifications for the member.
	_, err = st.UpdateCurrentUser(ctx, store.UpdateCurrentUserInput{
		UserID: member.ID,
		NotificationSettings: &store.NotificationSettings{
			PushoverEnabled: true,
			PushoverUserKey: "m12345678901234567890123456789",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("expected at least one channel")
	}
	channel := channels[0]

	// Create a message and check that the member is a recipient.
	msg, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "test message",
	})
	if err != nil {
		t.Fatal(err)
	}

	recipients, err := st.ListPushNotificationRecipients(ctx, msg.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, r := range recipients {
		if r.UserID == member.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected member to be a push notification recipient")
	}

	// Mute the channel for the member.
	err = st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID:  channel.ID,
		UserID:     member.ID,
		Preference: store.ChannelNotifyMuted,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check that the member is no longer a recipient.
	recipients2, err := st.ListPushNotificationRecipients(ctx, msg.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range recipients2 {
		if r.UserID == member.ID {
			t.Fatal("expected member to NOT be a push notification recipient after muting channel")
		}
	}

	if err := st.UpsertChannelNotificationSettings(ctx, store.ChannelNotificationInput{
		ChannelID: channel.ID, UserID: member.ID, Preference: store.ChannelNotifyMentions,
	}); err != nil {
		t.Fatal(err)
	}
	withoutMention, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID, AuthorID: owner.ID, Body: "nothing to flag",
	})
	if err != nil {
		t.Fatal(err)
	}
	recipients, err = st.ListPushNotificationRecipients(ctx, withoutMention.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, recipient := range recipients {
		if recipient.UserID == member.ID {
			t.Fatal("expected mentions-only member to be skipped without an @mention")
		}
	}
	withMention, mentionEvent, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID, AuthorID: owner.ID, Body: "hello @member",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMessage(ctx, store.UpdateMessageInput{
		MessageID: withMention.ID, UserID: owner.ID, Body: "mention removed after creation",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID: member.ID, DisplayName: member.DisplayName, Handle: "renamed-member",
	}); err != nil {
		t.Fatal(err)
	}
	recipients, err = st.ListPushNotificationRecipients(ctx, withMention.ID, mentionEvent.MentionedUserIDs)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, recipient := range recipients {
		if recipient.UserID == member.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mentions-only member to receive a matching @mention")
	}
}
