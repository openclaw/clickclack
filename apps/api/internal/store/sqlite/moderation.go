package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func normalizeWorkspaceRole(role string) string {
	switch role {
	case store.WorkspaceRoleOwner, store.WorkspaceRoleModerator, store.WorkspaceRoleMember, store.WorkspaceRoleGuest, store.WorkspaceRoleBot:
		return role
	default:
		return store.WorkspaceRoleMember
	}
}

func validWorkspaceRole(role string) bool {
	switch role {
	case store.WorkspaceRoleOwner, store.WorkspaceRoleModerator, store.WorkspaceRoleMember, store.WorkspaceRoleGuest:
		return true
	default:
		return false
	}
}

func roleRank(role string) int {
	switch role {
	case store.WorkspaceRoleOwner:
		return 5
	case store.WorkspaceRoleBot:
		return 4
	case store.WorkspaceRoleModerator:
		return 3
	case store.WorkspaceRoleMember:
		return 2
	case store.WorkspaceRoleGuest:
		return 1
	default:
		return 0
	}
}

func memberRoleTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) (string, error) {
	return storedb.New(tx).RequireMembershipRole(ctx, storedb.RequireMembershipRoleParams{WorkspaceID: workspaceID, UserID: userID})
}

func (s *Store) memberRole(ctx context.Context, workspaceID, userID string) (string, error) {
	return s.q.RequireMembershipRole(ctx, storedb.RequireMembershipRoleParams{WorkspaceID: workspaceID, UserID: userID})
}

func requireModeratorTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	role, err := memberRoleTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role != store.WorkspaceRoleOwner && role != store.WorkspaceRoleModerator {
		return store.ErrModerationRestricted
	}
	return requireNoModerationBlockTx(ctx, tx, workspaceID, userID)
}

func requireNonGuestTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	role, err := memberRoleTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role == store.WorkspaceRoleGuest {
		return store.ErrModerationRestricted
	}
	return nil
}

func requireGuestChannelAccessTx(ctx context.Context, tx *sql.Tx, workspaceID, channelID, userID string) error {
	role, err := memberRoleTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	if role != store.WorkspaceRoleGuest {
		return nil
	}
	name, err := storedb.New(tx).ChannelNameForAccess(ctx, storedb.ChannelNameForAccessParams{ID: channelID, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if name != "guest" {
		return store.ErrModerationRestricted
	}
	return nil
}

func (s *Store) requireGuestChannelAccess(ctx context.Context, workspaceID, channelID, userID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return requireGuestChannelAccessTx(ctx, tx, workspaceID, channelID, userID)
}

func (s *Store) CanPublishEphemeral(ctx context.Context, workspaceID, channelID, directConversationID, userID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if directConversationID != "" {
		return requireCanSendDirectTx(ctx, tx, workspaceID, userID)
	}
	if channelID != "" {
		return requireCanPostTx(ctx, tx, workspaceID, channelID, userID)
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	return requireNoModerationBlockTx(ctx, tx, workspaceID, userID)
}

func requireNoModerationBlockTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	var timeoutUntil, blockedAt sql.NullString
	row, err := storedb.New(tx).GetMemberModerationState(ctx, storedb.GetMemberModerationStateParams{WorkspaceID: workspaceID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	timeoutUntil = row.TimeoutUntil
	blockedAt = row.BlockedAt
	if blockedAt.Valid && blockedAt.String != "" {
		return fmt.Errorf("%w: member is blocked", store.ErrModerationRestricted)
	}
	if timeoutUntil.Valid && timeoutUntil.String != "" {
		timeoutAt, err := time.Parse(time.RFC3339Nano, timeoutUntil.String)
		if err != nil {
			return err
		}
		if timeoutAt.After(time.Now().UTC()) {
			return fmt.Errorf("%w: member is timed out until %s", store.ErrModerationRestricted, timeoutUntil.String)
		}
	}
	return nil
}

func requireCanPostTx(ctx context.Context, tx *sql.Tx, workspaceID, channelID, userID string) error {
	role, err := memberRoleTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	if role != store.WorkspaceRoleGuest {
		return nil
	}
	if err := requireGuestChannelAccessTx(ctx, tx, workspaceID, channelID, userID); err != nil {
		return err
	}
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	count, err := storedb.New(tx).CountRecentWorkspaceMessagesByAuthor(ctx, storedb.CountRecentWorkspaceMessagesByAuthorParams{
		WorkspaceID: workspaceID,
		AuthorID:    userID,
		Cutoff:      cutoff,
	})
	if err != nil {
		return err
	}
	if count >= int64(store.GuestPostLimit) {
		return store.ErrPostRateLimited
	}
	return nil
}

func requireCanSendDirectTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	role, err := memberRoleTx(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	if err := requireNoModerationBlockTx(ctx, tx, workspaceID, userID); err != nil {
		return err
	}
	if role == store.WorkspaceRoleGuest {
		return store.ErrModerationRestricted
	}
	return nil
}

func postsRemainingTx(ctx context.Context, q storedb.DBTX, workspaceID, userID, role string) (int, int, error) {
	if role != store.WorkspaceRoleGuest {
		return 0, 0, nil
	}
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	count, err := storedb.New(q).CountRecentWorkspaceMessagesByAuthor(ctx, storedb.CountRecentWorkspaceMessagesByAuthorParams{
		WorkspaceID: workspaceID,
		AuthorID:    userID,
		Cutoff:      cutoff,
	})
	if err != nil {
		return 0, 0, err
	}
	remaining := store.GuestPostLimit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, store.GuestPostLimit, nil
}

func (s *Store) ListWorkspaceMembers(ctx context.Context, workspaceID, actorUserID string) ([]store.MemberModeration, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireModeratorTx(ctx, tx, workspaceID, actorUserID); err != nil {
		return nil, err
	}
	rows, err := storedb.New(tx).ListWorkspaceMembersForModeration(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := []store.MemberModeration{}
	for _, row := range rows {
		item := store.MemberModeration{
			WorkspaceID: workspaceID,
			User: store.User{
				ID:          row.ID,
				Kind:        row.Kind,
				OwnerUserID: row.OwnerUserID,
				DisplayName: row.DisplayName,
				Handle:      row.Handle,
				AvatarURL:   row.AvatarUrl,
				CreatedAt:   row.CreatedAt,
			},
			Role:           row.Role,
			ModerationNote: row.ModerationNote,
			ModerationBy:   row.ModerationBy,
			ModerationAt:   row.ModerationAt,
		}
		if row.TimeoutUntil != "" {
			timeoutUntil := row.TimeoutUntil
			item.TimeoutUntil = &timeoutUntil
		}
		if row.BlockedAt != "" {
			blockedAt := row.BlockedAt
			item.BlockedAt = &blockedAt
		}
		item.PostsRemaining, item.PostLimit, err = postsRemainingTx(ctx, tx, workspaceID, item.User.ID, item.Role)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) UpdateMemberModeration(ctx context.Context, input store.UpdateMemberModerationInput) (store.MemberModeration, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	defer tx.Rollback()
	if err := requireModeratorTx(ctx, tx, input.WorkspaceID, input.ActorUserID); err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	if input.TargetUserID == input.ActorUserID {
		return store.MemberModeration{}, store.Event{}, errors.New("moderators cannot moderate themselves")
	}
	targetRole, err := memberRoleTx(ctx, tx, input.WorkspaceID, input.TargetUserID)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	if targetRole == store.WorkspaceRoleOwner {
		return store.MemberModeration{}, store.Event{}, errors.New("owners cannot be moderated")
	}
	actorRole, err := memberRoleTx(ctx, tx, input.WorkspaceID, input.ActorUserID)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	if roleRank(targetRole) >= roleRank(actorRole) {
		return store.MemberModeration{}, store.Event{}, errors.New("target role is too high for this moderator")
	}
	if input.Role != "" {
		if !validWorkspaceRole(input.Role) {
			return store.MemberModeration{}, store.Event{}, fmt.Errorf("invalid workspace role %q", input.Role)
		}
		role := normalizeWorkspaceRole(input.Role)
		if role == store.WorkspaceRoleOwner {
			return store.MemberModeration{}, store.Event{}, errors.New("owner role cannot be assigned here")
		}
		if roleRank(role) >= roleRank(actorRole) {
			return store.MemberModeration{}, store.Event{}, errors.New("role is too high for this moderator")
		}
		if err := storedb.New(tx).UpdateWorkspaceMemberRole(ctx, storedb.UpdateWorkspaceMemberRoleParams{Role: role, WorkspaceID: input.WorkspaceID, UserID: input.TargetUserID}); err != nil {
			return store.MemberModeration{}, store.Event{}, err
		}
		targetRole = role
	}
	nowValue := now()
	var blockedAt sql.NullString
	if input.Blocked != nil && *input.Blocked {
		blockedAt = sqlText(nowValue)
	}
	if input.Blocked != nil && !*input.Blocked {
		blockedAt = sql.NullString{}
	}
	timeoutUntil := sql.NullString{}
	if input.TimeoutUntil != nil && *input.TimeoutUntil != "" {
		timeoutAt, err := time.Parse(time.RFC3339Nano, *input.TimeoutUntil)
		if err != nil {
			return store.MemberModeration{}, store.Event{}, err
		}
		timeoutUntil = sqlText(timeoutAt.UTC().Format(time.RFC3339Nano))
	}
	moderationParams := storedb.UpsertMemberModerationParams{
		WorkspaceID:  input.WorkspaceID,
		UserID:       input.TargetUserID,
		TimeoutUntil: timeoutUntil,
		BlockedAt:    blockedAt,
		ModerationBy: sqlText(input.ActorUserID),
		ModerationAt: nowValue,
	}
	if input.ModerationNote != nil {
		err = storedb.New(tx).UpsertMemberModerationWithNote(ctx, storedb.UpsertMemberModerationWithNoteParams{
			WorkspaceID:    moderationParams.WorkspaceID,
			UserID:         moderationParams.UserID,
			TimeoutUntil:   moderationParams.TimeoutUntil,
			BlockedAt:      moderationParams.BlockedAt,
			ModerationNote: *input.ModerationNote,
			ModerationBy:   moderationParams.ModerationBy,
			ModerationAt:   moderationParams.ModerationAt,
		})
	} else {
		err = storedb.New(tx).UpsertMemberModeration(ctx, moderationParams)
	}
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	qtx := storedb.New(tx)
	clearParams := storedb.ClearMemberTimeoutParams{WorkspaceID: input.WorkspaceID, UserID: input.TargetUserID, ModerationBy: sqlText(input.ActorUserID), ModerationAt: nowValue}
	if input.ClearTimeout {
		if err := qtx.ClearMemberTimeout(ctx, clearParams); err != nil {
			return store.MemberModeration{}, store.Event{}, err
		}
	}
	if input.Blocked != nil && !*input.Blocked {
		if err := qtx.ClearMemberBlocked(ctx, storedb.ClearMemberBlockedParams(clearParams)); err != nil {
			return store.MemberModeration{}, store.Event{}, err
		}
	}
	recipients, err := moderationEventRecipientsTx(ctx, tx, input.WorkspaceID, input.TargetUserID)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, input.WorkspaceID, "", "member.moderation_updated", nil, map[string]string{"user_id": input.TargetUserID, "role": targetRole}, recipients)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	members, err := s.ListWorkspaceMembers(ctx, input.WorkspaceID, input.ActorUserID)
	if err != nil {
		return store.MemberModeration{}, store.Event{}, err
	}
	for _, member := range members {
		if member.User.ID == input.TargetUserID {
			return member, event, nil
		}
	}
	return store.MemberModeration{}, store.Event{}, sql.ErrNoRows
}

func moderationEventRecipientsTx(ctx context.Context, tx *sql.Tx, workspaceID, targetUserID string) ([]string, error) {
	rows, err := storedb.New(tx).ListWorkspaceMembersForModeration(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{targetUserID: {}}
	recipients := []string{targetUserID}
	now := time.Now().UTC()
	for _, row := range rows {
		if row.Role != store.WorkspaceRoleOwner && row.Role != store.WorkspaceRoleModerator {
			continue
		}
		active, err := isActiveModerationRecipient(row.TimeoutUntil, row.BlockedAt, now)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		recipients = append(recipients, row.ID)
	}
	return recipients, nil
}

func isActiveModerationRecipient(timeoutUntil, blockedAt string, now time.Time) (bool, error) {
	if blockedAt != "" {
		return false, nil
	}
	if timeoutUntil == "" {
		return true, nil
	}
	timeoutAt, err := time.Parse(time.RFC3339Nano, timeoutUntil)
	if err != nil {
		return false, err
	}
	return !timeoutAt.After(now), nil
}

func (s *Store) UserHasNonGuestMembership(ctx context.Context, userID string) (bool, error) {
	_, err := s.q.UserHasNonGuestMembership(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
