package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func TestStoreValidationAndAdminHelpers(t *testing.T) {
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
	workspace := workspaces[0]
	pushoverUserKey := "abcdefghijklmnopqrstuvwxyz1234"
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          owner.ID,
		PushoverEnabled: true,
		PushoverUserKey: pushoverUserKey,
	}); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]

	if _, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ClickClack", Slug: workspace.Slug}, owner.ID); err == nil {
		t.Fatal("expected duplicate workspace slug error")
	}
	if _, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Guests", Slug: "guests"}, owner.ID); err == nil {
		t.Fatal("expected reserved workspace slug error")
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID}); err == nil {
		t.Fatal("expected empty message error")
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "read me"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkChannelRead(ctx, channel.ID, owner.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: channel.ID, AuthorID: owner.ID, Body: "nope"}); err == nil {
		t.Fatal("expected missing root message error")
	}
	if results, err := st.SearchMessages(ctx, workspace.ID, "", owner.ID, "", 10); err != nil || len(results) != 0 {
		t.Fatalf("expected empty search results, got %#v err=%v", results, err)
	}
	invite, err := st.CreateInvite(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	link, err := st.CreateMagicLink(ctx, "magic@example.com", "Magic User")
	if err != nil {
		t.Fatal(err)
	}
	magicUser, session, err := st.ConsumeMagicLink(ctx, link.Token)
	if err != nil {
		t.Fatal(err)
	}
	if magicUser.DisplayName != "Magic User" || session.Token == "" {
		t.Fatalf("unexpected magic auth result: %#v %#v", magicUser, session)
	}
	if _, err := st.UpdateNotificationSettings(ctx, store.UpdateNotificationSettingsInput{
		UserID:          magicUser.ID,
		PushoverEnabled: true,
		PushoverUserKey: "abcdefghijklmnopqrstuvwxyz1234",
	}); err != nil {
		t.Fatal(err)
	}
	sessionUser, err := st.GetSessionUser(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if sessionUser.ID != magicUser.ID {
		t.Fatalf("expected session user %s, got %s", magicUser.ID, sessionUser.ID)
	}
	if sessionUser.NotificationSettings == nil || !sessionUser.NotificationSettings.PushoverEnabled || sessionUser.NotificationSettings.PushoverUserKey == "" {
		t.Fatalf("expected session user notification settings, got %#v", sessionUser.NotificationSettings)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, magicUser.ID, "member"); err != nil {
		t.Fatal(err)
	}
	readDM, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{magicUser.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: readDM.ID, AuthorID: magicUser.ID, Body: "dm read me"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkDirectRead(ctx, readDM.ID, owner.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ConsumeMagicLink(ctx, link.Token); err == nil {
		t.Fatal("expected consumed magic link error")
	}
	if _, err := st.CreateMagicLink(ctx, "", "No Email"); err == nil {
		t.Fatal("expected missing email error")
	}
	bot, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Owner Bot",
		Handle:      "owner-bot",
		Scopes:      []string{"messages:write", "realtime:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bot.Kind != "bot" || bot.OwnerUserID != owner.ID || botToken.Token == "" {
		t.Fatalf("unexpected bot/token: %#v %#v", bot, botToken)
	}
	botAuth, err := st.GetBotTokenAuth(ctx, botToken.Token)
	if err != nil {
		t.Fatal(err)
	}
	if botAuth.User.ID != bot.ID || botAuth.WorkspaceID != workspace.ID {
		t.Fatalf("unexpected bot auth: %#v", botAuth)
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, OwnerUserID: bot.ID, DisplayName: "Nested"}); err == nil {
		t.Fatal("expected bot owner rejection")
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Bad Scope", Scopes: []string{"bad:scope"}}); err == nil {
		t.Fatal("expected bad scope rejection")
	}
	if _, _, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Duplicate Handle", Handle: "owner-bot"}); err == nil {
		t.Fatal("expected duplicate bot handle rejection")
	}
	upload, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "secret.txt",
		ContentType: "text/plain",
		ByteSize:    6,
		StoragePath: "/tmp/clickclack-secret-upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := st.ExportJSON(ctx, &exported); err != nil {
		t.Fatal(err)
	}
	var exportBody map[string][]map[string]any
	if err := json.Unmarshal(exported.Bytes(), &exportBody); err != nil {
		t.Fatal(err)
	}
	if len(exportBody["auth_magic_links"]) == 0 || len(exportBody["sessions"]) == 0 {
		t.Fatalf("expected auth tables in export, got keys %#v", exportBody)
	}
	if len(exportBody["channel_reads"]) == 0 || len(exportBody["direct_reads"]) == 0 {
		t.Fatalf("expected read receipt tables in export, got keys %#v", exportBody)
	}
	if string(exported.Bytes()) == "" || bytes.Contains(exported.Bytes(), []byte(link.Token)) || bytes.Contains(exported.Bytes(), []byte(session.Token)) || bytes.Contains(exported.Bytes(), []byte(invite.Token)) {
		t.Fatalf("export leaked bearer token: %s", exported.String())
	}
	for _, secret := range []string{tokenHash(link.Token), tokenHash(session.Token), tokenHash(botToken.Token), upload.StoragePath, pushoverUserKey} {
		if bytes.Contains(exported.Bytes(), []byte(secret)) {
			t.Fatalf("export leaked sensitive stored value %q: %s", secret, exported.String())
		}
	}
	if len(exportBody["user_notification_settings"]) == 0 {
		t.Fatalf("expected user_notification_settings in export, got keys %#v", exportBody)
	}
	if exportBody["user_notification_settings"][0]["pushover_user_key"] != "[redacted]" {
		t.Fatalf("pushover user key export was not redacted: %#v", exportBody["user_notification_settings"][0])
	}
	if exportBody["auth_magic_links"][0]["token"] != "[redacted]" || exportBody["auth_magic_links"][0]["token_hash"] != "[redacted]" {
		t.Fatalf("magic link export was not redacted: %#v", exportBody["auth_magic_links"][0])
	}
	if exportBody["sessions"][0]["token"] != "[redacted]" || exportBody["sessions"][0]["token_hash"] != "[redacted]" {
		t.Fatalf("session export was not redacted: %#v", exportBody["sessions"][0])
	}
	if len(exportBody["bot_tokens"]) == 0 {
		t.Fatalf("expected bot_tokens in export, got keys %#v", exportBody)
	}
	if exportBody["bot_tokens"][0]["token_hash"] != "[redacted]" {
		t.Fatalf("bot token export was not redacted: %#v", exportBody["bot_tokens"][0])
	}
	if len(exportBody["invites"]) == 0 {
		t.Fatalf("expected invites in export, got keys %#v", exportBody)
	}
	if exportBody["invites"][0]["token"] != "[redacted]" {
		t.Fatalf("invite token export was not redacted: %#v", exportBody["invites"][0])
	}
	if exportBody["uploads"][0]["storage_path"] != "[redacted]" {
		t.Fatalf("upload export was not redacted: %#v", exportBody["uploads"][0])
	}
	if err := st.Backup(ctx, filepath.Join(t.TempDir(), "backup.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DROP TABLE sessions`); err != nil {
		t.Fatal(err)
	}
	if err := st.ExportJSON(ctx, &bytes.Buffer{}); err == nil {
		t.Fatal("expected export failure")
	}

	second, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Second", Email: "second@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{second.ID}}); err == nil {
		t.Fatal("expected dm membership error for second user")
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, second.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	dms, err := st.ListDirectConversations(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundDM := false
	for _, item := range dms {
		if item.ID == dm.ID {
			foundDM = true
		}
	}
	if !foundDM {
		t.Fatalf("unexpected dm list: %#v", dms)
	}
	if _, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: second.ID}); err == nil {
		t.Fatal("expected empty dm message error")
	}
}

func TestUploadQuotaRejectsOwnerWorkspaceOverflow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "quota-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "almost-full.bin",
		ContentType: "application/octet-stream",
		ByteSize:    store.UploadQuotaBytesPerUserWorkspace - 1,
		StoragePath: "/tmp/almost-full.bin",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CanCreateUpload(ctx, workspace.ID, owner.ID, 2); !errors.Is(err, store.ErrUploadQuotaExceeded) {
		t.Fatalf("expected quota denial, got %v", err)
	}
	if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "overflow.bin",
		ContentType: "application/octet-stream",
		ByteSize:    2,
		StoragePath: "/tmp/overflow.bin",
	}); !errors.Is(err, store.ErrUploadQuotaExceeded) {
		t.Fatalf("expected quota denial on create, got %v", err)
	}
}

func TestUploadNonceLookupSurvivesWorkspaceMembershipRemoval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "stale-upload-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	created, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Nonce:       "stale-workspace-nonce",
		Filename:    "proof.txt",
		ContentType: "text/plain",
		ByteSize:    5,
		StoragePath: "/tmp/proof.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	found, err := st.GetUploadByNonce(ctx, owner.ID, created.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != created.ID || found.WorkspaceID != workspace.ID {
		t.Fatalf("unexpected upload lookup after membership removal: %#v", found)
	}
}

func TestUploadNonceValidationCountsCharacters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "unicode-upload-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	valid := strings.Repeat("é", 128)
	if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Nonce:       valid,
		Filename:    "valid.txt",
		ContentType: "text/plain",
		ByteSize:    1,
		StoragePath: "/tmp/valid.txt",
	}); err != nil {
		t.Fatalf("expected 128-character nonce to remain valid: %v", err)
	}
	if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Nonce:       strings.Repeat("é", 129),
		Filename:    "invalid.txt",
		ContentType: "text/plain",
		ByteSize:    1,
		StoragePath: "/tmp/invalid.txt",
	}); err == nil || !strings.Contains(err.Error(), "nonce is too long") {
		t.Fatalf("expected 129-character nonce rejection, got %v", err)
	}
	for _, nonce := range []string{string([]byte{0xff}), "invalid\x00nonce"} {
		if _, err := st.CreateUpload(ctx, store.CreateUploadInput{
			WorkspaceID: workspace.ID,
			OwnerID:     owner.ID,
			Nonce:       nonce,
			Filename:    "invalid.txt",
			ContentType: "text/plain",
			ByteSize:    1,
			StoragePath: "/tmp/invalid.txt",
		}); err == nil {
			t.Fatalf("expected database-invalid nonce rejection for %q", nonce)
		}
	}
}

func TestUploadReservationClaimsOwnerNonceBeforeQuota(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "reservation-nonce-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	otherWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "Other Upload Claims"}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := st.ReserveUploadQuota(ctx, workspace.ID, owner.ID, "claim-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Nonce != "claim-1" {
		t.Fatalf("reservation lost nonce claim: %#v", reservation)
	}
	if _, err := st.ReserveUploadQuota(ctx, workspace.ID, owner.ID, "claim-1", 10); !errors.Is(err, store.ErrUploadNonceInProgress) {
		t.Fatalf("expected same-workspace in-progress claim, got %v", err)
	}
	if _, err := st.ReserveUploadQuota(ctx, otherWorkspace.ID, owner.ID, "claim-1", 10); !errors.Is(err, store.ErrUploadNonceConflict) {
		t.Fatalf("expected cross-workspace nonce conflict, got %v", err)
	}
	if err := st.ReleaseUploadQuotaReservation(ctx, reservation.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReserveUploadQuota(ctx, otherWorkspace.ID, owner.ID, "claim-1", 10); err != nil {
		t.Fatalf("released nonce claim remained locked: %v", err)
	}
}

func TestUploadReservationReclaimsExpiredNonceClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "expired-reservation-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO upload_quota_reservations
			(id, workspace_id, owner_id, client_nonce, byte_size, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"uqr_expired",
		workspace.ID,
		owner.ID,
		"expired-claim",
		10,
		"2000-01-01T00:00:00.000000000Z",
		"2000-01-01T00:01:00.000000000Z",
	); err != nil {
		t.Fatal(err)
	}

	reservation, err := st.ReserveUploadQuota(ctx, workspace.ID, owner.ID, "expired-claim", 10)
	if err != nil {
		t.Fatalf("expired nonce claim was not reclaimed: %v", err)
	}
	if reservation.ID == "uqr_expired" || reservation.Nonce != "expired-claim" {
		t.Fatalf("unexpected replacement reservation: %#v", reservation)
	}
}

func TestReservedUploadRechecksModerationOnFinalize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "reserved-upload-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Upload Member", Email: "reserved-upload-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	reservation, err := st.ReserveUploadQuota(ctx, workspace.ID, member.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	blocked := true
	if _, _, err := st.UpdateMemberModeration(ctx, store.UpdateMemberModerationInput{WorkspaceID: workspace.ID, ActorUserID: owner.ID, TargetUserID: member.ID, Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateReservedUpload(ctx, reservation.ID, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     member.ID,
		Filename:    "blocked.bin",
		ContentType: "application/octet-stream",
		ByteSize:    1,
		StoragePath: "/tmp/blocked.bin",
	})
	if !errors.Is(err, store.ErrModerationRestricted) {
		t.Fatalf("expected moderation recheck failure, got %v", err)
	}
}

func TestReservedUploadRejectsNegativeFinalizeSize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Owner", "reserved-upload-negative-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	reservation, err := st.ReserveUploadQuota(ctx, workspace.ID, owner.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateReservedUpload(ctx, reservation.ID, store.CreateUploadInput{
		WorkspaceID: workspace.ID,
		OwnerID:     owner.ID,
		Filename:    "negative.bin",
		ContentType: "application/octet-stream",
		ByteSize:    -1,
		StoragePath: "/tmp/negative.bin",
	})
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("expected negative size rejection, got %v", err)
	}
	quota, err := st.UploadQuota(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quota.UsedBytes != reservation.ByteSize || quota.UsedCount != 1 {
		t.Fatalf("negative finalize changed quota unexpectedly: %#v", quota)
	}
}

func TestAuthTimestampComparisonsParseRFC3339Nano(t *testing.T) {
	t.Parallel()
	current, err := time.Parse(time.RFC3339Nano, "2026-01-01T00:00:00.123Z")
	if err != nil {
		t.Fatal(err)
	}
	if authTimestampExpired("2026-01-01T00:00:00.1234Z", current) {
		t.Fatal("expected nanosecond-precision expiration to remain valid")
	}
	later, err := time.Parse(time.RFC3339Nano, "2026-01-01T00:00:00.1234Z")
	if err != nil {
		t.Fatal(err)
	}
	if !authTimestampExpired("2026-01-01T00:00:00.123Z", later) {
		t.Fatal("expected older nanosecond-precision expiration to be expired")
	}

	ctx := context.Background()
	st := newTestStore(t)
	user, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Fractional Time", Email: "fractional@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	sessionToken := "fractional-session-token"
	if err := st.q.InsertSession(ctx, storedb.InsertSessionParams{
		ID:        "ses_fractional",
		Token:     "ses_fractional",
		TokenHash: tokenHash(sessionToken),
		UserID:    user.ID,
		CreatedAt: "2026-01-01T00:00:00Z",
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	sessionUser, err := st.GetSessionUser(ctx, sessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if sessionUser.ID != user.ID {
		t.Fatalf("expected session user %s, got %s", user.ID, sessionUser.ID)
	}

	magicToken := "fractional-magic-token"
	magicExpiresAt := "2026-01-01T00:00:00.1234Z"
	if err := st.q.InsertMagicLink(ctx, storedb.InsertMagicLinkParams{
		ID:          "mln_fractional",
		Token:       "mln_fractional",
		TokenHash:   tokenHash(magicToken),
		Email:       "fractional@example.com",
		DisplayName: "Fractional Time",
		CreatedAt:   "2026-01-01T00:00:00Z",
		ExpiresAt:   magicExpiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.q.MarkMagicLinkUsed(ctx, storedb.MarkMagicLinkUsedParams{UsedAt: sqlText("2026-01-01T00:00:00.123Z"), ID: "mln_fractional", Now: "2026-01-01T00:00:00.123Z"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("expected magic link to be marked used, got %d rows", rows)
	}

	if err := st.q.InsertMagicLink(ctx, storedb.InsertMagicLinkParams{
		ID:          "mln_fractional_expired",
		Token:       "mln_fractional_expired",
		TokenHash:   tokenHash("fractional-expired-token"),
		Email:       "fractional-expired@example.com",
		DisplayName: "Fractional Expired",
		CreatedAt:   "2026-01-01T00:00:00Z",
		ExpiresAt:   "2026-01-01T00:00:00.123Z",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err = st.q.MarkMagicLinkUsed(ctx, storedb.MarkMagicLinkUsedParams{UsedAt: sqlText("2026-01-01T00:00:00.1234Z"), ID: "mln_fractional_expired", Now: "2026-01-01T00:00:00.1234Z"})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expected expired magic link to remain unused, got %d rows", rows)
	}
}

func TestBotStoreValidationAndAuthEdges(t *testing.T) {
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
	workspace := workspaces[0]
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Outsider", Email: "outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	createFailures := []struct {
		name  string
		input store.CreateBotInput
	}{
		{"missing workspace", store.CreateBotInput{DisplayName: "Missing Workspace"}},
		{"missing display name", store.CreateBotInput{WorkspaceID: workspace.ID}},
		{"display name too long", store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: strings.Repeat("a", 81)}},
		{"invalid handle", store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Bad Handle", Handle: "!"}},
		{"invalid avatar", store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Bad Avatar", AvatarURL: "ftp://example.com/avatar.png"}},
		{"missing owner", store.CreateBotInput{WorkspaceID: workspace.ID, OwnerUserID: "usr_missing", DisplayName: "Missing Owner"}},
		{"owner not member", store.CreateBotInput{WorkspaceID: workspace.ID, OwnerUserID: outsider.ID, DisplayName: "Outsider Bot"}},
		{"unknown scope", store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Unknown Scope", Scopes: []string{"bad:scope"}}},
	}
	for _, tc := range createFailures {
		t.Run("create_"+tc.name, func(t *testing.T) {
			if _, _, err := st.CreateBot(ctx, tc.input); err == nil {
				t.Fatal("expected CreateBot error")
			}
		})
	}

	bot, token, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Default Scope Bot",
		TokenName:   " ",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.Name != "default" || len(token.Scopes) == 0 {
		t.Fatalf("expected default token metadata, got %#v", token)
	}
	auth, err := st.GetBotTokenAuth(ctx, " "+token.Token+" ")
	if err != nil {
		t.Fatal(err)
	}
	if auth.User.ID != bot.ID || auth.TokenID != token.ID || auth.WorkspaceID != workspace.ID {
		t.Fatalf("unexpected bot token auth: %#v", auth)
	}

	if _, err := st.GetBotTokenAuth(ctx, "not-a-bot-token"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected non-bot token miss, got %v", err)
	}
	if _, err := st.GetBotTokenAuth(ctx, "ccb_missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected missing bot token miss, got %v", err)
	}

	_, revokedToken, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Revoked Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_tokens SET revoked_at = ? WHERE id = ?`, now(), revokedToken.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBotTokenAuth(ctx, revokedToken.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected revoked token miss, got %v", err)
	}

	_, malformedToken, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Malformed Scope Bot", CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE bot_tokens SET scopes_json = ? WHERE id = ?`, `{`, malformedToken.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBotTokenAuth(ctx, malformedToken.Token); err == nil {
		t.Fatal("expected malformed scope JSON error")
	}

	ownerMembershipBot, ownerMembershipToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Owner Membership Bot",
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBotTokenAuth(ctx, ownerMembershipToken.Token); err == nil {
		t.Fatal("expected missing bot owner membership error")
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspace.ID, ownerMembershipBot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetBotTokenAuth(ctx, ownerMembershipToken.Token); err == nil {
		t.Fatal("expected missing bot membership error")
	}
}

func TestOpenRejectsBadDirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open("sqlite://" + filepath.Join(path, "db.sqlite")); err == nil {
		t.Fatal("expected bad directory error")
	}
}

func TestStoreClosedDatabaseErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	if err := st.db.Close(); err != nil {
		t.Fatal(err)
	}

	errorCases := []struct {
		name string
		fn   func() error
	}{
		{"migrate", func() error { return st.Migrate(ctx) }},
		{"create user", func() error {
			_, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "x"})
			return err
		}},
		{"first user", func() error {
			_, err := st.FirstUser(ctx)
			return err
		}},
		{"get user", func() error {
			_, err := st.GetUser(ctx, "usr_missing")
			return err
		}},
		{"list workspaces", func() error {
			_, err := st.ListWorkspaces(ctx, "usr_missing")
			return err
		}},
		{"create workspace", func() error {
			_, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "x"}, "usr_missing")
			return err
		}},
		{"create channel", func() error {
			_, _, err := st.CreateChannel(ctx, store.CreateChannelInput{})
			return err
		}},
		{"create message", func() error {
			_, _, err := st.CreateMessage(ctx, store.CreateMessageInput{})
			return err
		}},
		{"create reply", func() error {
			_, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{})
			return err
		}},
		{"add reaction", func() error {
			_, err := st.AddReaction(ctx, store.CreateReactionInput{})
			return err
		}},
		{"remove reaction", func() error {
			_, err := st.RemoveReaction(ctx, store.CreateReactionInput{})
			return err
		}},
		{"create upload", func() error {
			_, err := st.CreateUpload(ctx, store.CreateUploadInput{})
			return err
		}},
		{"attach upload", func() error {
			_, err := st.AttachUpload(ctx, store.AttachUploadInput{})
			return err
		}},
		{"create dm", func() error {
			_, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{})
			return err
		}},
		{"create dm message", func() error {
			_, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{})
			return err
		}},
		{"resolve route", func() error {
			_, err := st.ResolveRouteTarget(ctx, "usr_missing", "TMISSING", "CMISSING")
			return err
		}},
		{"resolve legacy route", func() error {
			_, err := st.ResolveLegacyRouteTarget(ctx, "usr_missing", "wsp_missing", "chn_missing")
			return err
		}},
		{"ensure thread route", func() error {
			_, err := st.EnsureThreadRouteID(ctx, "usr_missing", "msg_missing")
			return err
		}},
		{"backfill route ids", func() error {
			return st.backfillRouteIDs(ctx)
		}},
		{"assign route id", func() error {
			return st.assignRouteID(ctx, "workspaces", "wsp_missing", 'T')
		}},
		{"magic link", func() error {
			_, err := st.CreateMagicLink(ctx, "x@example.com", "x")
			return err
		}},
		{"identity", func() error {
			_, err := st.UpsertIdentityUser(ctx, store.UpsertIdentityUserInput{Provider: "github", ProviderSubject: "1"})
			return err
		}},
		{"session", func() error {
			_, err := st.CreateSession(ctx, "usr_missing")
			return err
		}},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}
}

func TestAuthTokenHashMigrationBackfillsLegacyTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0012_auth_token_hashes.sql")
	expiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	mustExecSQL(t, ctx, st, `INSERT INTO users (id, display_name, created_at) VALUES ('usr_legacy', 'Legacy', ?)`, now())
	mustExecSQL(t, ctx, st, `INSERT INTO identities (id, user_id, provider, provider_subject, email, created_at) VALUES ('idn_legacy', 'usr_legacy', 'magic', 'legacy@example.com', 'legacy@example.com', ?)`, now())
	mustExecSQL(t, ctx, st, `INSERT INTO auth_magic_links (id, token, email, created_at, expires_at) VALUES ('aml_legacy', 'legacy-link-token', 'legacy@example.com', ?, ?)`, now(), expiresAt)
	mustExecSQL(t, ctx, st, `INSERT INTO sessions (id, token, user_id, created_at, expires_at) VALUES ('ses_legacy', 'legacy-session-token', 'usr_legacy', ?, ?)`, now(), expiresAt)

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM auth_magic_links WHERE token = id AND token_hash = ?`, tokenHash("legacy-link-token")); got != 1 {
		t.Fatalf("expected legacy magic-link token hash backfill and token scrub, got %d", got)
	}
	if got := scalarCount(t, ctx, st, `SELECT COUNT(*) FROM sessions WHERE token = id AND token_hash = ?`, tokenHash("legacy-session-token")); got != 1 {
		t.Fatalf("expected legacy session token hash backfill and token scrub, got %d", got)
	}
	if user, _, err := st.ConsumeMagicLink(ctx, "legacy-link-token"); err != nil || user.ID != "usr_legacy" {
		t.Fatalf("expected legacy magic link token to remain valid, user=%#v err=%v", user, err)
	}
	if user, err := st.GetSessionUser(ctx, "legacy-session-token"); err != nil || user.ID != "usr_legacy" {
		t.Fatalf("expected legacy session token to remain valid, user=%#v err=%v", user, err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}
