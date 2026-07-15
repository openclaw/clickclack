package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const postgresSearchCreatedAtKey = `CASE
	WHEN length(m.created_at) = 20 AND right(m.created_at, 1) = 'Z'
		THEN left(m.created_at, 19) || '.000000000Z'
	WHEN length(m.created_at) BETWEEN 22 AND 30
		AND substr(m.created_at, 20, 1) = '.'
		AND right(m.created_at, 1) = 'Z'
		THEN left(m.created_at, 20) ||
			rpad(substr(m.created_at, 21, length(m.created_at) - 21), 9, '0') ||
			'Z'
	ELSE to_char(m.created_at::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
END`

func (s *Store) SearchMessagePage(ctx context.Context, page store.SearchPageRequest) (store.SearchPage, error) {
	req, err := store.NormalizeSearchPageRequest(page)
	if err != nil {
		return store.SearchPage{}, err
	}
	cursor, _, err := store.DecodeSearchCursor(req.Cursor, req)
	if err != nil {
		return store.SearchPage{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.SearchPage{}, err
	}
	defer tx.Rollback()

	role, err := memberRoleTx(ctx, tx, req.WorkspaceID, req.UserID)
	if err != nil {
		return store.SearchPage{}, err
	}
	scopeWhere, scopeArgs, err := postgresSearchScope(ctx, tx, req, role, 4)
	if err != nil {
		return store.SearchPage{}, err
	}
	if req.Query == "" {
		return store.SearchPage{Results: []store.SearchHit{}}, tx.Commit()
	}
	markers, err := store.NewSearchMarkers()
	if err != nil {
		return store.SearchPage{}, err
	}
	headlineOptions := "StartSel=" + markers.Start +
		", StopSel=" + markers.End +
		", MaxWords=32, MinWords=16, MaxFragments=2, FragmentDelimiter=…"

	args := []any{req.Query, headlineOptions, req.WorkspaceID}
	args = append(args, scopeArgs...)

	rankExpression := "ts_rank_cd(to_tsvector('simple', m.body), plainto_tsquery('simple', $1))"
	cursorWhere := ""
	orderBy := "rank DESC, created_at_key DESC, m.id DESC"
	if req.Cursor != "" {
		start := len(args) + 1
		switch req.Sort {
		case store.SearchSortRelevance:
			cursorWhere = fmt.Sprintf(`AND (
				%s < $%d
				OR (%s = $%d AND %s < $%d)
				OR (%s = $%d AND %s = $%d AND m.id < $%d)
			)`,
				rankExpression, start,
				rankExpression, start+1, postgresSearchCreatedAtKey, start+2,
				rankExpression, start+3, postgresSearchCreatedAtKey, start+4, start+5,
			)
			args = append(args, cursor.Rank, cursor.Rank, cursor.CreatedAt, cursor.Rank, cursor.CreatedAt, cursor.MessageID)
		case store.SearchSortNewest:
			cursorWhere = fmt.Sprintf(`AND (
				%s < $%d
				OR (%s = $%d AND m.id < $%d)
			)`, postgresSearchCreatedAtKey, start, postgresSearchCreatedAtKey, start+1, start+2)
			args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.MessageID)
		}
	}
	if req.Sort == store.SearchSortNewest {
		orderBy = "created_at_key DESC, m.id DESC"
	}
	limitPlaceholder := len(args) + 1
	args = append(args, req.Limit+1)

	rows, err := tx.QueryContext(ctx, `
		SELECT m.id,
		       m.workspace_id,
		       COALESCE(m.channel_id, ''),
		       COALESCE(c.name, ''),
		       COALESCE(m.direct_conversation_id, ''),
		       m.author_id,
		       u.kind,
		       u.owner_user_id,
		       u.display_name,
		       u.handle,
		       u.avatar_url,
		       u.created_at,
		       author_tombstone.former_handle,
		       author_tombstone.deleted_at,
		       m.parent_message_id,
		       m.thread_root_id,
		       m.channel_seq,
		       m.thread_seq,
		       m.created_at,
		       `+postgresSearchCreatedAtKey+` AS created_at_key,
		       m.edited_at,
		       COALESCE(thread_state.reply_count, 0),
		       thread_state.last_reply_at,
		       `+rankExpression+` AS rank,
		       ts_headline(
		         'simple',
		         m.body,
		         plainto_tsquery('simple', $1),
		         $2
		       ) AS snippet
		FROM messages m
		LEFT JOIN channels c ON c.id = m.channel_id AND c.workspace_id = m.workspace_id
		JOIN users u ON u.id = m.author_id
		LEFT JOIN bot_tombstones author_tombstone ON author_tombstone.bot_user_id = u.id
		LEFT JOIN thread_state ON thread_state.root_message_id = m.id
		WHERE m.workspace_id = $3
		  AND to_tsvector('simple', m.body) @@ plainto_tsquery('simple', $1)
		  AND m.deleted_at IS NULL
		  AND m.kind = 'message'
		  `+scopeWhere+`
		  `+cursorWhere+`
		ORDER BY `+orderBy+`
		LIMIT $`+fmt.Sprint(limitPlaceholder), args...)
	if err != nil {
		return store.SearchPage{}, err
	}
	defer rows.Close()

	resultRows := make([]store.SearchPageEntry, 0, req.Limit+1)
	for rows.Next() {
		row, markedSnippet, err := scanSearchPageRow(rows)
		if err != nil {
			return store.SearchPage{}, err
		}
		row.Hit.Snippet, row.Hit.Highlights, err = store.ParseSearchSnippetWithMarkers(markedSnippet, markers)
		if err != nil {
			return store.SearchPage{}, err
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return store.SearchPage{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.SearchPage{}, err
	}
	return store.BuildSearchPage(req, resultRows)
}

func postgresSearchScope(ctx context.Context, tx *sql.Tx, req store.SearchPageRequest, role string, firstPlaceholder int) (string, []any, error) {
	switch {
	case req.ChannelID != "":
		var channelName string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM channels WHERE id = $1 AND workspace_id = $2`, req.ChannelID, req.WorkspaceID).Scan(&channelName); err != nil {
			return "", nil, err
		}
		if role == store.WorkspaceRoleGuest && channelName != store.GuestChannelName {
			return "", nil, store.ErrModerationRestricted
		}
		return fmt.Sprintf("AND m.channel_id = $%d AND m.direct_conversation_id IS NULL", firstPlaceholder), []any{req.ChannelID}, nil
	case req.DirectConversationID != "":
		var workspaceID string
		if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM direct_conversations WHERE id = $1`, req.DirectConversationID).Scan(&workspaceID); err != nil {
			return "", nil, err
		}
		if workspaceID != req.WorkspaceID {
			return "", nil, fmt.Errorf("%w: direct conversation is not in workspace", store.ErrInvalidSearch)
		}
		if err := requireDirectAccessTx(ctx, tx, req.DirectConversationID, req.UserID); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf(`AND m.direct_conversation_id = $%d
			AND EXISTS (
				SELECT 1
				FROM direct_conversation_members dcm
				WHERE dcm.conversation_id = m.direct_conversation_id
				  AND dcm.user_id = $%d
			)`, firstPlaceholder, firstPlaceholder+1), []any{req.DirectConversationID, req.UserID}, nil
	default:
		if role == store.WorkspaceRoleGuest {
			return fmt.Sprintf("AND m.direct_conversation_id IS NULL AND m.channel_id IS NOT NULL AND c.name = $%d", firstPlaceholder), []any{store.GuestChannelName}, nil
		}
		return "AND m.direct_conversation_id IS NULL AND m.channel_id IS NOT NULL", nil, nil
	}
}

func scanSearchPageRow(row scanner) (store.SearchPageEntry, string, error) {
	var result store.SearchPageEntry
	var parentMessageID, editedAt, authorOwnerID, authorFormerHandle, authorDeletedAt, lastReplyAt sql.NullString
	var channelSeq, threadSeq sql.NullInt64
	var markedSnippet string
	err := row.Scan(
		&result.Hit.ID,
		&result.Hit.WorkspaceID,
		&result.Hit.ChannelID,
		&result.Hit.ChannelName,
		&result.Hit.DirectConversationID,
		&result.Hit.Author.ID,
		&result.Hit.Author.Kind,
		&authorOwnerID,
		&result.Hit.Author.DisplayName,
		&result.Hit.Author.Handle,
		&result.Hit.Author.AvatarURL,
		&result.Hit.Author.CreatedAt,
		&authorFormerHandle,
		&authorDeletedAt,
		&parentMessageID,
		&result.Hit.ThreadRootID,
		&channelSeq,
		&threadSeq,
		&result.Hit.CreatedAt,
		&result.CursorCreatedAt,
		&editedAt,
		&result.Hit.ReplyCount,
		&lastReplyAt,
		&result.Rank,
		&markedSnippet,
	)
	if err != nil {
		return store.SearchPageEntry{}, "", err
	}
	if parentMessageID.Valid {
		result.Hit.ParentMessageID = &parentMessageID.String
	}
	if channelSeq.Valid {
		result.Hit.ChannelSeq = &channelSeq.Int64
	}
	if threadSeq.Valid {
		result.Hit.ThreadSeq = &threadSeq.Int64
	}
	if editedAt.Valid {
		result.Hit.EditedAt = &editedAt.String
	}
	if authorOwnerID.Valid {
		result.Hit.Author.OwnerUserID = authorOwnerID.String
	}
	if authorFormerHandle.Valid {
		result.Hit.Author.FormerHandle = authorFormerHandle.String
	}
	if authorDeletedAt.Valid {
		result.Hit.Author.DeletedAt = &authorDeletedAt.String
	}
	if lastReplyAt.Valid {
		result.Hit.LastReplyAt = &lastReplyAt.String
	}
	return result, markedSnippet, nil
}
