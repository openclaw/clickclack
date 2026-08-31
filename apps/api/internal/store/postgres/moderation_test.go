package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestMessageNonceRecoveryFollowsCurrentAccess(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

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
	var generalChannelID, guestChannelID string
	for _, channel := range channels {
		switch channel.Name {
		case "general":
			generalChannelID = channel.ID
		case "guest":
			guestChannelID = channel.ID
		}
	}
	if generalChannelID == "" || guestChannelID == "" {
		t.Fatalf("expected general and guest channels, got %#v", channels)
	}
	general, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: member.ID, Body: "before demotion", Nonce: "nonce-replay"})
	if err != nil {
		t.Fatal(err)
	}
	guest, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: guestChannelID, AuthorID: member.ID, Body: "waiting room", Nonce: "guest-nonce"})
	if err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: member.ID, MemberIDs: []string{moderator.ID}})
	if err != nil {
		t.Fatal(err)
	}
	direct, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: member.ID, Body: "direct before demotion", Nonce: "direct-nonce"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		message store.Message
		denied  bool
	}{
		{"channel", general, true},
		{"direct", direct, true},
		{"guest channel", guest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name+" before demotion", func(t *testing.T) {
			if got, err := st.GetMessageByNonce(ctx, member.ID, " "+tc.message.Nonce+" "); err != nil || got.ID != tc.message.ID {
				t.Fatalf("expected sender to recover message %s, got %s: %v", tc.message.ID, got.ID, err)
			}
			if _, err := st.GetMessageByNonce(ctx, moderator.ID, tc.message.Nonce); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("expected other author's nonce to be hidden, got %v", err)
			}
		})
	}
	for _, nonce := range []string{"", "missing"} {
		if _, err := st.GetMessageByNonce(ctx, member.ID, nonce); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("expected absent nonce %q to return no rows, got %v", nonce, err)
		}
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: moderator.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name+" after demotion", func(t *testing.T) {
			var wantErr error
			if tc.denied {
				wantErr = store.ErrModerationRestricted
			}
			if _, err := st.GetMessage(ctx, tc.message.ID, member.ID); !errors.Is(err, wantErr) {
				t.Fatalf("unexpected message access after demotion: %v", err)
			}
			got, err := st.GetMessageByNonce(ctx, member.ID, tc.message.Nonce)
			if !errors.Is(err, wantErr) {
				t.Fatalf("expected nonce recovery error %v, got %v", wantErr, err)
			}
			if !tc.denied && got.ID != tc.message.ID {
				t.Fatalf("expected permitted message %s, got %s", tc.message.ID, got.ID)
			}
		})
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: generalChannelID, AuthorID: member.ID, Body: "before demotion", Nonce: "nonce-replay"}); !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected replayed hidden channel nonce to be blocked, got %v", err)
	}
}

func TestModerationHydrationFailureRollsBack(t *testing.T) {
	ctx := t.Context()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "hydrate-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "hydrate-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	before, err := st.LatestEventCursor(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Only guest quota hydration needs this table; moderation authorization and writes still work.
	if _, err := st.db.ExecContext(ctx, `DROP TABLE slash_command_invocations`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: member.ID, Role: store.WorkspaceRoleGuest}); err == nil {
		t.Fatal("expected quota hydration failure")
	}
	role, err := st.memberRole(ctx, workspace.ID, member.ID)
	if err != nil || role != store.WorkspaceRoleMember {
		t.Fatalf("failed moderation committed role %q: %v", role, err)
	}
	after, err := st.LatestEventCursor(ctx, workspace.ID, owner.ID)
	if err != nil || after != before {
		t.Fatalf("failed moderation committed an event: before=%s after=%s error=%v", before, after, err)
	}
}
