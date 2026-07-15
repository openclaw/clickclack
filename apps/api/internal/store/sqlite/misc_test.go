package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestStoreMiscBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	raw, err := Open(filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	st := newTestStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	unnamed, err := st.CreateUser(ctx, store.CreateUserInput{})
	if err != nil {
		t.Fatal(err)
	}
	if unnamed.DisplayName != "Local User" {
		t.Fatalf("unexpected default user: %#v", unnamed)
	}
	updatedOwner, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      owner.ID,
		DisplayName: "Peter Steinberger",
		Handle:      "@steipete",
		AvatarURL:   "https://example.com/avatar.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedOwner.Handle != "steipete" || updatedOwner.AvatarURL == "" {
		t.Fatalf("unexpected profile update: %#v", updatedOwner)
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: unnamed.ID, DisplayName: "Other", Handle: "STEIPETE"}); err == nil {
		t.Fatal("expected duplicate handle error")
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: owner.ID, DisplayName: " ", Handle: "bad"}); err == nil {
		t.Fatal("expected empty display name error")
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: owner.ID, DisplayName: "Peter", AvatarURL: "ftp://example.com/a.png"}); err == nil {
		t.Fatal("expected bad avatar URL error")
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: owner.ID, DisplayName: strings.Repeat("x", 81)}); err == nil {
		t.Fatal("expected long display name error")
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: owner.ID, DisplayName: "Peter", AvatarURL: "https://" + strings.Repeat("a", 500)}); err == nil {
		t.Fatal("expected long avatar URL error")
	}
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{UserID: "usr_missing", DisplayName: "Missing"}); err == nil {
		t.Fatal("expected missing profile user error")
	}
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{UserID: owner.ID, PushoverEnabled: true}); err == nil {
		t.Fatal("expected missing pushover key error")
	}
	settings, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          owner.ID,
		PushoverEnabled: true,
		PushoverUserKey: "u12345678901234567890123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PushoverEnabled || settings.PushoverUserKey == "" {
		t.Fatalf("unexpected notification settings: %#v", settings)
	}
	ownerWithSettings, err := st.GetUser(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ownerWithSettings.NotificationSettings == nil || !ownerWithSettings.NotificationSettings.PushoverEnabled {
		t.Fatalf("expected hydrated notification settings: %#v", ownerWithSettings)
	}
	identityUser, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{
		Provider:        "github",
		ProviderSubject: "42",
		Email:           "octo@example.com",
		DisplayName:     "Octo",
		AvatarURL:       "https://example.com/a.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	againIdentity, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if againIdentity.ID != identityUser.ID {
		t.Fatalf("expected existing identity user, got %#v", againIdentity)
	}
	session, err := st.CreateSession(ctx, identityUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserID != identityUser.ID || session.Token == "" {
		t.Fatalf("unexpected session: %#v", session)
	}
	if _, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{}); err == nil {
		t.Fatal("expected missing identity error")
	}
	fallbackIdentity, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "fallback"})
	if err != nil {
		t.Fatal(err)
	}
	if fallbackIdentity.DisplayName != "github:fallback" {
		t.Fatalf("unexpected fallback identity display: %#v", fallbackIdentity)
	}
	emailIdentity, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "email", Email: "email@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if emailIdentity.DisplayName != "email@example.com" {
		t.Fatalf("unexpected email identity display: %#v", emailIdentity)
	}
	if _, err := st.CreateSession(ctx, "usr_missing"); err == nil {
		t.Fatal("expected missing session user error")
	}
	untitled, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if untitled.Name != "Untitled" || untitled.Slug != "untitled" {
		t.Fatalf("unexpected default workspace: %#v", untitled)
	}
	if err := st.AddWorkspaceMember(ctx, untitled.ID, unnamed.ID, ""); err != nil {
		t.Fatal(err)
	}
	joined, err := st.EnsureDefaultWorkspaceMember(ctx, identityUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Name != "ClickClack" {
		t.Fatalf("expected first workspace, got %#v", joined)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "edited root"})
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := st.ListPushNotificationRecipients(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 0 {
		t.Fatalf("author should not receive own push notification: %#v", recipients)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspaces[0].ID, UserID: owner.ID, MemberIDs: []string{identityUser.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dmMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: identityUser.ID, Body: "dm push"})
	if err != nil {
		t.Fatal(err)
	}
	recipients, err = st.ListPushNotificationRecipients(ctx, dmMessage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].UserID != owner.ID {
		t.Fatalf("expected opted-in DM recipient only, got %#v", recipients)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE messages SET edited_at = created_at, deleted_at = created_at WHERE id = ?`, root.ID); err != nil {
		t.Fatal(err)
	}
	page, err := st.ListMessages(ctx, channels[0].ID, owner.ID, store.MessagePageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	messages := page.Messages
	if messages[0].EditedAt == nil || messages[0].DeletedAt == nil {
		t.Fatalf("expected edited/deleted fields, got %#v", messages[0])
	}

	authors := []store.User{owner}
	for _, name := range []string{"One", "Two", "Three", "Four"} {
		user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: name, Email: name + "@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, user.ID, "member"); err != nil {
			t.Fatal(err)
		}
		authors = append(authors, user)
	}
	var reply store.Message
	for i, author := range []store.User{authors[0], authors[1], authors[0], authors[2], authors[3], authors[4]} {
		reply, _, _, err = st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID: root.ID,
			AuthorID:      author.ID,
			Body:          "reply searchable",
		})
		if err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE messages SET edited_at = created_at, deleted_at = created_at WHERE id = ?`, reply.ID); err != nil {
		t.Fatal(err)
	}
	_, replies, threadState, err := st.GetThread(ctx, root.ID, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.GetThread(ctx, root.ID, owner.ID, 0); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 6 || len(threadState.LastReplyAuthorIDs) != 3 {
		t.Fatalf("unexpected thread compaction: replies=%d state=%#v", len(replies), threadState)
	}
	recipients, err = st.ListPushNotificationRecipients(ctx, reply.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].UserID != owner.ID {
		t.Fatalf("expected opted-in non-author recipient, got %#v", recipients)
	}
	if replies[len(replies)-1].EditedAt == nil || replies[len(replies)-1].DeletedAt == nil {
		t.Fatalf("expected edited/deleted reply fields, got %#v", replies[len(replies)-1])
	}
	results, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: workspaces[0].ID,
		UserID:      owner.ID,
		Query:       "reply",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) == 0 || results.Results[0].ParentMessageID == nil || results.Results[0].ThreadSeq == nil {
		t.Fatalf("expected reply search result with thread fields, got %#v", results)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspaces[0].ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	recipients, err = st.ListPushNotificationRecipients(ctx, dmMessage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 0 {
		t.Fatalf("revoked DM member should not receive push notifications, got %#v", recipients)
	}
}

func TestEnsureDefaultWorkspaceMemberCreatesWorkspace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "GitHub User", Email: "github@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := st.EnsureDefaultWorkspaceMember(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Name != "ClickClack" {
		t.Fatalf("unexpected workspace: %#v", workspace)
	}
	workspaces, err := st.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("expected default workspace membership, got %#v", workspaces)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].Name != "general" {
		t.Fatalf("expected general channel, got %#v", channels)
	}
	if updated, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channels[0].ID, UserID: user.ID, Name: "renamed"}); err != nil || updated.Name != "renamed" {
		t.Fatalf("expected first default workspace member to administer channels, got %#v %v", updated, err)
	}
	if again, err := st.EnsureDefaultWorkspaceMember(ctx, user.ID); err != nil || again.ID != workspace.ID {
		t.Fatalf("expected idempotent default membership, got %#v %v", again, err)
	}

	closed := newTestStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.EnsureDefaultWorkspaceMember(ctx, user.ID); err == nil {
		t.Fatal("expected closed db default workspace error")
	}

	withWorkspace := newTestStore(t)
	owner, err := withWorkspace.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withWorkspace.EnsureDefaultWorkspaceMember(ctx, "usr_missing"); err == nil {
		t.Fatal("expected missing user membership error")
	}
	ownerWorkspaces, err := withWorkspace.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerWorkspaces) != 1 {
		t.Fatalf("unexpected owner workspaces: %#v", ownerWorkspaces)
	}
}

func TestEnsureDefaultWorkspaceMemberUsesExplicitDefaultSlug(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	owner, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Private Owner", Email: "private-owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	privateWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Private", Slug: "private"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	allowedUser, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Allowed Org", Email: "allowed-org@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := st.EnsureDefaultWorkspaceMember(ctx, allowedUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Slug != "clickclack" || joined.ID == privateWorkspace.ID {
		t.Fatalf("expected explicit clickclack default workspace, got %#v private=%#v", joined, privateWorkspace)
	}
	workspaces, err := st.ListWorkspaces(ctx, allowedUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != joined.ID {
		t.Fatalf("expected allowed user to join only default workspace, got %#v", workspaces)
	}
	channels, err := st.ListChannels(ctx, joined.ID, allowedUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected default channel, got %#v", channels)
	}
	if updated, _, err := st.UpdateChannel(ctx, store.UpdateChannelInput{ChannelID: channels[0].ID, UserID: allowedUser.ID, Name: "default-admin"}); err != nil || updated.Name != "default-admin" {
		t.Fatalf("expected first default workspace member to administer new default workspace, got %#v %v", updated, err)
	}
}
