package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/sqlite/storedb"
)

func (s *Store) GetThreadPage(ctx context.Context, rootMessageID, userID string, req store.ThreadPageRequest) (store.ThreadPage, error) {
	normalized, _, err := normalizeMessagePageRequest(req.MessagePageRequest)
	if err != nil {
		return store.ThreadPage{}, err
	}
	req.MessagePageRequest = normalized
	if req.Latest && (req.BeforeSeq != nil || req.AfterSeq != nil || req.AroundSeq != nil) {
		return store.ThreadPage{}, fmt.Errorf("%w: latest and cursors are mutually exclusive", store.ErrInvalidMessagePage)
	}
	root, err := getMessage(ctx, s.db, rootMessageID)
	if err != nil {
		return store.ThreadPage{}, err
	}
	if root.ParentMessageID != nil {
		return store.ThreadPage{}, errors.New("thread root must be a root message")
	}
	if err := s.requireMessageAccess(ctx, root, userID); err != nil {
		return store.ThreadPage{}, err
	}
	root, err = s.EnsureThreadRouteID(ctx, userID, root.ID)
	if err != nil {
		return store.ThreadPage{}, err
	}
	query := func(lower, upper int64, descending bool, limit int) ([]store.Message, error) {
		direction := int64(0)
		if descending {
			direction = 1
		}
		rows, err := s.q.ListThreadReplyPage(ctx, storedb.ListThreadReplyPageParams{RootID: root.ID, LowerSeq: sql.NullInt64{Int64: lower, Valid: true}, UpperSeq: sql.NullInt64{Int64: upper, Valid: true}, DescendingOrder: direction, PageLimit: int64(limit)})
		if err != nil {
			return nil, err
		}
		replies := make([]store.Message, 0, len(rows))
		for _, row := range rows {
			replies = append(replies, threadReplyFromRow(row))
		}
		return replies, nil
	}
	var replies []store.Message
	switch {
	case req.BeforeSeq != nil:
		replies, err = query(0, *req.BeforeSeq-1, true, req.Limit)
	case req.AfterSeq != nil:
		replies, err = query(*req.AfterSeq, math.MaxInt64, false, req.Limit)
	case req.AroundSeq != nil:
		var left, right []store.Message
		left, err = query(0, *req.AroundSeq, true, req.Limit)
		if err != nil {
			break
		}
		takeLeft := min(len(left), (req.Limit+1)/2)
		right, err = query(*req.AroundSeq, math.MaxInt64, false, req.Limit-takeLeft)
		if err != nil {
			break
		}
		takeLeft = min(len(left), req.Limit-len(right))
		replies = append(left[len(left)-takeLeft:], right...)
	default:
		replies, err = query(0, math.MaxInt64, req.Latest, req.Limit)
	}
	if err != nil {
		return store.ThreadPage{}, err
	}
	messages, err := s.hydrateAttachments(ctx, append([]store.Message{root}, replies...))
	if err != nil {
		return store.ThreadPage{}, err
	}
	messages, err = s.hydrateReactions(ctx, userID, messages)
	if err != nil {
		return store.ThreadPage{}, err
	}
	state, err := getThreadState(ctx, s.db, root.ID)
	if err != nil {
		return store.ThreadPage{}, err
	}
	page := store.ThreadPage{Root: messages[0], Replies: messages[1:], ThreadState: state}
	if len(replies) > 0 {
		page.OldestSeq = *replies[0].ThreadSeq
		page.NewestSeq = *replies[len(replies)-1].ThreadSeq
		edges, err := s.q.ThreadReplyEdges(ctx, storedb.ThreadReplyEdgesParams{RootID: root.ID, OldestSeq: sql.NullInt64{Int64: page.OldestSeq, Valid: true}, NewestSeq: sql.NullInt64{Int64: page.NewestSeq, Valid: true}})
		if err != nil {
			return store.ThreadPage{}, err
		}
		page.HasOlder = edges.HasOlder
		page.HasNewer = edges.HasNewer
	}
	return page, nil
}

func threadReplyFromRow(row storedb.ListThreadReplyPageRow) store.Message {
	message := store.Message{
		ID: row.ID, RouteID: stringFromNull(row.RouteID), WorkspaceID: row.WorkspaceID, ChannelID: stringFromNull(row.ChannelID), DirectConversationID: stringFromNull(row.DirectConversationID),
		AuthorID: row.AuthorID, ParentMessageID: ptrFromNull(row.ParentMessageID), ThreadRootID: row.ThreadRootID, TopicID: stringFromNull(row.TopicID),
		Body: row.Body, BodyFormat: row.BodyFormat, CreatedAt: row.CreatedAt, EditedAt: ptrFromNull(row.EditedAt), DeletedAt: ptrFromNull(row.DeletedAt),
		Kind: row.Kind, TurnID: stringFromNull(row.TurnID), Nonce: row.ClientNonce,
		QuotedMessageID: ptrFromNull(row.QuotedMessageID), QuotedBodySnapshot: row.QuotedBodySnapshot, QuotedAuthorID: ptrFromNull(row.QuotedAuthorID),
		Author: &store.User{ID: row.AuthorID, Kind: row.AuthorKind, OwnerUserID: stringFromNull(row.AuthorOwnerID), DisplayName: row.AuthorName, Handle: row.AuthorHandle, AvatarURL: row.AuthorAvatar, CreatedAt: row.AuthorCreated, FormerHandle: stringFromNull(row.AuthorFormerHandle), DeletedAt: ptrFromNull(row.AuthorDeleted)},
	}
	if row.ThreadSeq.Valid {
		message.ThreadSeq = &row.ThreadSeq.Int64
	}
	if row.ChannelSeq.Valid {
		message.ChannelSeq = &row.ChannelSeq.Int64
	}
	if row.QuotedAuthorID.Valid {
		message.QuotedAuthor = &store.User{ID: row.QuotedAuthorID.String, Kind: row.QuoteKind.String, OwnerUserID: row.QuoteOwnerID.String, DisplayName: row.QuoteName.String, Handle: row.QuoteHandle.String, AvatarURL: row.QuoteAvatar.String, CreatedAt: row.QuoteCreated.String, FormerHandle: row.QuoteFormerHandle.String, DeletedAt: ptrFromNull(row.QuoteDeleted)}
	}
	return message
}
