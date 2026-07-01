package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

const directConversationMemberHydrationBatchSize = 500

func (s *Store) ListDirectConversations(ctx context.Context, workspaceID, userID string) ([]store.DirectConversation, error) {
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	role, err := s.memberRole(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	if role == store.WorkspaceRoleGuest {
		return []store.DirectConversation{}, nil
	}
	rows, err := s.q.ListDirectConversations(ctx, storedb.ListDirectConversationsParams{ReaderUserID: userID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	out := make([]store.DirectConversation, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeDirectConversationFromList(row))
	}
	ids := make([]string, 0, len(out))
	for _, dm := range out {
		ids = append(ids, dm.ID)
	}
	membersByConversation, err := s.directConversationMembersByConversationIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Members = membersByConversation[out[i].ID]
	}
	return out, nil
}

func (s *Store) GetDirectConversation(ctx context.Context, conversationID, userID string) (store.DirectConversation, error) {
	row, err := s.q.GetDirectConversation(ctx, storedb.GetDirectConversationParams{ReaderUserID: userID, ConversationID: conversationID})
	if err != nil {
		return store.DirectConversation{}, err
	}
	role, err := s.memberRole(ctx, row.WorkspaceID, userID)
	if err != nil {
		return store.DirectConversation{}, err
	}
	if role == store.WorkspaceRoleGuest {
		return store.DirectConversation{}, store.ErrModerationRestricted
	}
	dm := storeDirectConversationFromGet(row)
	members, err := s.directConversationMembers(ctx, dm.ID)
	if err != nil {
		return store.DirectConversation{}, err
	}
	dm.Members = members
	return dm, nil
}

func (s *Store) CreateDirectConversation(ctx context.Context, input store.CreateDirectConversationInput) (store.DirectConversation, error) {
	memberIDs := append([]string{input.UserID}, input.MemberIDs...)
	memberIDs = compactStrings(memberIDs)
	if len(memberIDs) < 2 {
		return store.DirectConversation{}, errors.New("direct conversation needs at least two members")
	}
	if len(memberIDs) > store.MaxDirectConversationMembers {
		return store.DirectConversation{}, errors.New("direct conversation has too many members")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.DirectConversation{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if err := requireCanSendDirectTx(ctx, tx, input.WorkspaceID, input.UserID); err != nil {
		return store.DirectConversation{}, err
	}
	for _, memberID := range memberIDs {
		if err := requireCanSendDirectTx(ctx, tx, input.WorkspaceID, memberID); err != nil {
			return store.DirectConversation{}, err
		}
	}
	memberSetKey := store.DirectConversationMemberSetKey(memberIDs)
	if memberSetKey != "" {
		existing, err := qtx.FindOneToOneDirectConversation(ctx, storedb.FindOneToOneDirectConversationParams{
			WorkspaceID:  input.WorkspaceID,
			FirstUserID:  memberIDs[0],
			SecondUserID: memberIDs[1],
		})
		if err == nil {
			if err := qtx.UnhideDirectConversation(ctx, storedb.UnhideDirectConversationParams{ConversationID: existing.ID, UserID: input.UserID}); err != nil {
				return store.DirectConversation{}, err
			}
			if err := tx.Commit(); err != nil {
				return store.DirectConversation{}, err
			}
			return s.hydrateDirectConversation(ctx, store.DirectConversation{
				ID:          existing.ID,
				RouteID:     existing.RouteID,
				WorkspaceID: existing.WorkspaceID,
				CreatedAt:   existing.CreatedAt,
			})
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return store.DirectConversation{}, err
		}
	}
	dm := store.DirectConversation{ID: newID("dm"), WorkspaceID: input.WorkspaceID, CreatedAt: now()}
	inserted := false
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		routeID, err := newRouteID('D')
		if err != nil {
			return store.DirectConversation{}, err
		}
		dm.RouteID = routeID
		insertedRows, err := qtx.InsertDirectConversation(ctx, storedb.InsertDirectConversationParams{
			ID:           dm.ID,
			RouteID:      sqlText(dm.RouteID),
			WorkspaceID:  dm.WorkspaceID,
			CreatedAt:    dm.CreatedAt,
			MemberSetKey: sqlOptionalText(memberSetKey),
		})
		if err != nil {
			return store.DirectConversation{}, err
		}
		if insertedRows == 0 {
			if memberSetKey != "" {
				existing, lookupErr := qtx.GetDirectConversationByMemberSetKey(ctx, storedb.GetDirectConversationByMemberSetKeyParams{
					WorkspaceID:  input.WorkspaceID,
					MemberSetKey: sqlText(memberSetKey),
				})
				if lookupErr == nil {
					if err := qtx.UnhideDirectConversation(ctx, storedb.UnhideDirectConversationParams{ConversationID: existing.ID, UserID: input.UserID}); err != nil {
						return store.DirectConversation{}, err
					}
					if err := tx.Commit(); err != nil {
						return store.DirectConversation{}, err
					}
					return s.hydrateDirectConversation(ctx, store.DirectConversation{
						ID:          existing.ID,
						RouteID:     existing.RouteID,
						WorkspaceID: existing.WorkspaceID,
						CreatedAt:   existing.CreatedAt,
					})
				}
				if !errors.Is(lookupErr, sql.ErrNoRows) {
					return store.DirectConversation{}, lookupErr
				}
			}
			continue
		}
		inserted = true
		break
	}
	if !inserted {
		return store.DirectConversation{}, errors.New("could not create direct conversation route_id after collision retries")
	}
	for _, memberID := range memberIDs {
		if err := qtx.InsertDirectConversationMember(ctx, storedb.InsertDirectConversationMemberParams{ConversationID: dm.ID, UserID: memberID, CreatedAt: dm.CreatedAt}); err != nil {
			return store.DirectConversation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return store.DirectConversation{}, err
	}
	return s.hydrateDirectConversation(ctx, dm)
}

func (s *Store) HideDirectConversation(ctx context.Context, conversationID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireDirectAccessTx(ctx, tx, conversationID, userID); err != nil {
		return err
	}
	if err := s.q.WithTx(tx).HideDirectConversation(ctx, storedb.HideDirectConversationParams{
		ConversationID: conversationID,
		UserID:         userID,
		HiddenAt:       now(),
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReopenDirectConversation(ctx context.Context, conversationID, userID string) (store.DirectConversation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.DirectConversation{}, err
	}
	defer tx.Rollback()
	if err := requireDirectAccessTx(ctx, tx, conversationID, userID); err != nil {
		return store.DirectConversation{}, err
	}
	qtx := s.q.WithTx(tx)
	if err := qtx.UnhideDirectConversation(ctx, storedb.UnhideDirectConversationParams{ConversationID: conversationID, UserID: userID}); err != nil {
		return store.DirectConversation{}, err
	}
	row, err := qtx.GetDirectConversation(ctx, storedb.GetDirectConversationParams{ReaderUserID: userID, ConversationID: conversationID})
	if err != nil {
		return store.DirectConversation{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.DirectConversation{}, err
	}
	return s.hydrateDirectConversation(ctx, storeDirectConversationFromGet(row))
}

func (s *Store) hydrateDirectConversation(ctx context.Context, dm store.DirectConversation) (store.DirectConversation, error) {
	members, err := s.directConversationMembers(ctx, dm.ID)
	if err != nil {
		return store.DirectConversation{}, err
	}
	dm.Members = members
	return dm, nil
}

func (s *Store) ListDirectMessages(ctx context.Context, conversationID, userID string, page store.MessagePageRequest) (store.MessagePage, error) {
	if err := s.requireDirectAccess(ctx, conversationID, userID); err != nil {
		return store.MessagePage{}, err
	}
	return s.listMessagePage(ctx, messagePageScope{
		where: "m.direct_conversation_id = $1 AND m.parent_message_id IS NULL",
		args:  []any{conversationID},
	}, page)
}

func (s *Store) CreateDirectMessage(ctx context.Context, input store.CreateDirectMessageInput) (store.Message, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	workspaceID, err := qtx.GetDirectConversationWorkspace(ctx, input.ConversationID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := requireDirectMembershipTx(ctx, tx, input.ConversationID, input.AuthorID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := requireCanSendDirectTx(ctx, tx, workspaceID, input.AuthorID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := lockMessageSequenceTx(ctx, tx, "direct", input.ConversationID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	seq, err := qtx.DirectNextSeq(ctx, input.ConversationID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	id := newID("msg")
	createdAt := now()
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return store.Message{}, store.Event{}, errors.New("message body is required")
	}
	nonce, err := normalizeClientNonce(input.Nonce)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	var quotedID, quotedAuthorID, quotedSnapshot string
	if input.QuotedMessageID != nil {
		quotedID = strings.TrimSpace(*input.QuotedMessageID)
	}
	if existing, err := getMessageByClientNonceTx(ctx, tx, input.AuthorID, nonce); err == nil {
		if existing.DirectConversationID != input.ConversationID || existing.ChannelID != "" || existing.ParentMessageID != nil || existing.Body != body || !sameQuotedMessageID(existing, quotedID) {
			return store.Message{}, store.Event{}, store.ErrClientNonceConflict
		}
		return existing, store.Event{}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Message{}, store.Event{}, err
	}
	if quotedID != "" {
		snap, authorID, err := resolveQuoteRefTx(ctx, tx, quotedID, quoteScope{kind: "dm", directConversationID: input.ConversationID})
		if err != nil {
			return store.Message{}, store.Event{}, err
		}
		quotedSnapshot = snap
		quotedAuthorID = authorID
	}
	if err := qtx.InsertDirectMessage(ctx, storedb.InsertDirectMessageParams{
		ID:                   id,
		WorkspaceID:          workspaceID,
		DirectConversationID: sqlText(input.ConversationID),
		AuthorID:             input.AuthorID,
		ThreadRootID:         id,
		ChannelSeq:           sqlInt64(seq),
		Body:                 body,
		CreatedAt:            createdAt,
		QuotedMessageID:      sqlOptionalText(quotedID),
		QuotedBodySnapshot:   quotedSnapshot,
		QuotedAuthorID:       sqlOptionalText(quotedAuthorID),
		ClientNonce:          nonce,
	}); err != nil {
		if existing, lookupErr := getMessageByClientNonceTx(ctx, tx, input.AuthorID, nonce); lookupErr == nil {
			if existing.DirectConversationID == input.ConversationID && existing.ChannelID == "" && existing.ParentMessageID == nil && existing.Body == body && sameQuotedMessageID(existing, quotedID) {
				return existing, store.Event{}, nil
			}
			return store.Message{}, store.Event{}, store.ErrClientNonceConflict
		}
		return store.Message{}, store.Event{}, err
	}
	if err := qtx.InsertThreadState(ctx, id); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := qtx.UnhideDirectConversationForMembers(ctx, input.ConversationID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	recipients, err := directConversationMemberIDsTx(ctx, tx, input.ConversationID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, workspaceID, "", "message.created", &seq, eventPayload(map[string]string{"message_id": id, "direct_conversation_id": input.ConversationID, "author_id": input.AuthorID}, nonce), recipients)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	msg, err := getMessageTx(ctx, tx, id)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	return msg, event, tx.Commit()
}

func (s *Store) requireDirectAccess(ctx context.Context, conversationID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return requireDirectAccessTx(ctx, tx, conversationID, userID)
}

func requireDirectAccessTx(ctx context.Context, tx *sql.Tx, conversationID, userID string) error {
	qtx := storedb.New(tx)
	workspaceID, err := qtx.GetDirectConversationWorkspace(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := requireDirectMembershipTx(ctx, tx, conversationID, userID); err != nil {
		return err
	}
	return requireNonGuestTx(ctx, tx, workspaceID, userID)
}

func requireDirectMembershipTx(ctx context.Context, tx *sql.Tx, conversationID, userID string) error {
	_, err := storedb.New(tx).RequireDirectMembership(ctx, storedb.RequireDirectMembershipParams{ConversationID: conversationID, UserID: userID})
	return err
}

func directConversationMemberIDsTx(ctx context.Context, tx *sql.Tx, conversationID string) ([]string, error) {
	return storedb.New(tx).DirectConversationMemberIDs(ctx, conversationID)
}

func (s *Store) directConversationMembers(ctx context.Context, conversationID string) ([]store.User, error) {
	rows, err := s.q.DirectConversationMembers(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	members := make([]store.User, 0, len(rows))
	for _, row := range rows {
		members = append(members, storeUserFromDirectConversationMember(row))
	}
	return members, nil
}

func (s *Store) directConversationMembersByConversationIDs(ctx context.Context, conversationIDs []string) (map[string][]store.User, error) {
	out := map[string][]store.User{}
	if len(conversationIDs) == 0 {
		return out, nil
	}
	for start := 0; start < len(conversationIDs); start += directConversationMemberHydrationBatchSize {
		end := min(start+directConversationMemberHydrationBatchSize, len(conversationIDs))
		batch := conversationIDs[start:end]
		placeholders := pgPlaceholders(len(batch), 1)
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT dcm.conversation_id, u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
			FROM direct_conversation_members dcm
			JOIN users u ON u.id = dcm.user_id
			WHERE dcm.conversation_id IN (`+placeholders+`)
			ORDER BY dcm.conversation_id, u.display_name`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var conversationID string
			var owner sql.NullString
			var member store.User
			if err := rows.Scan(&conversationID, &member.ID, &member.Kind, &owner, &member.DisplayName, &member.Handle, &member.AvatarURL, &member.CreatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if owner.Valid {
				member.OwnerUserID = owner.String
			}
			out[conversationID] = append(out[conversationID], member)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
