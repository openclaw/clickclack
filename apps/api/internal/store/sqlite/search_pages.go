package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const sqliteSearchCreatedAtKey = `CASE
	WHEN length(m.created_at) = 20 AND substr(m.created_at, -1) = 'Z'
		THEN substr(m.created_at, 1, 19) || '.000000000Z'
	WHEN length(m.created_at) BETWEEN 22 AND 30
		AND substr(m.created_at, 20, 1) = '.'
		AND substr(m.created_at, -1) = 'Z'
		THEN substr(m.created_at, 1, 20) ||
			substr(substr(m.created_at, 21, length(m.created_at) - 21) || '000000000', 1, 9) ||
			'Z'
	ELSE strftime('%Y-%m-%dT%H:%M:%fZ', m.created_at)
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

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.SearchPage{}, err
	}
	defer tx.Rollback()

	role, err := memberRoleTx(ctx, tx, req.WorkspaceID, req.UserID)
	if err != nil {
		return store.SearchPage{}, err
	}
	scopeWhere, scopeArgs, err := sqliteSearchScope(ctx, tx, req, role)
	if err != nil {
		return store.SearchPage{}, err
	}
	if req.Query == "" {
		return store.SearchPage{Results: []store.SearchHit{}}, tx.Commit()
	}
	compiledQuery := store.CompileSQLiteSearchQuery(req.WorkspaceID, req.Query)
	if compiledQuery == "" {
		return store.SearchPage{Results: []store.SearchHit{}}, tx.Commit()
	}
	markers, err := store.NewSearchMarkers()
	if err != nil {
		return store.SearchPage{}, err
	}

	cursorWhere := ""
	cursorArgs := []any{}
	rankExpression := "bm25(messages_fts)"
	innerOrderBy := "rank ASC, created_at_key DESC, m.id DESC"
	outerOrderBy := "ranked.rank ASC, ranked.created_at_key DESC, m.id DESC"
	if req.Cursor != "" {
		switch req.Sort {
		case store.SearchSortRelevance:
			cursorWhere = fmt.Sprintf(`AND (
				bm25(messages_fts) > ?
				OR (bm25(messages_fts) = ? AND %s < ?)
				OR (bm25(messages_fts) = ? AND %s = ? AND m.id < ?)
			)`, sqliteSearchCreatedAtKey, sqliteSearchCreatedAtKey)
			cursorArgs = append(cursorArgs, cursor.Rank, cursor.Rank, cursor.CreatedAt, cursor.Rank, cursor.CreatedAt, cursor.MessageID)
		case store.SearchSortNewest:
			cursorWhere = fmt.Sprintf(`AND (
				%s < ?
				OR (%s = ? AND m.id < ?)
			)`, sqliteSearchCreatedAtKey, sqliteSearchCreatedAtKey)
			cursorArgs = append(cursorArgs, cursor.CreatedAt, cursor.CreatedAt, cursor.MessageID)
		}
	}
	if req.Sort == store.SearchSortNewest {
		rankExpression = "0.0"
		innerOrderBy = "created_at_key DESC, m.id DESC"
		outerOrderBy = "ranked.created_at_key DESC, m.id DESC"
	}

	args := []any{req.WorkspaceID, compiledQuery}
	args = append(args, scopeArgs...)
	args = append(args, cursorArgs...)
	args = append(args, req.Limit+1)
	args = append(args, markers.Start, markers.End, compiledQuery)
	rows, err := tx.QueryContext(ctx, `
		WITH ranked AS (
			SELECT messages_fts.rowid AS fts_rowid,
			       m.id AS message_id,
			       `+sqliteSearchCreatedAtKey+` AS created_at_key,
			       `+rankExpression+` AS rank
			FROM messages_fts
			JOIN messages m ON m.id = messages_fts.message_id
			WHERE messages_fts.workspace_id = ?
			  AND messages_fts MATCH ?
			  AND m.deleted_at IS NULL
			  AND m.kind = 'message'
			  `+scopeWhere+`
			  `+cursorWhere+`
			ORDER BY `+innerOrderBy+`
			LIMIT ?
		)
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
		       ranked.created_at_key,
		       m.edited_at,
		       COALESCE(thread_state.reply_count, 0),
		       thread_state.last_reply_at,
		       ranked.rank,
		       snippet(messages_fts, 2, ?, ?, '…', 32)
		FROM ranked
		JOIN messages m ON m.id = ranked.message_id
		JOIN messages_fts ON messages_fts.rowid = ranked.fts_rowid
		LEFT JOIN channels c ON c.id = m.channel_id AND c.workspace_id = m.workspace_id
		JOIN users u ON u.id = m.author_id
		LEFT JOIN bot_tombstones author_tombstone ON author_tombstone.bot_user_id = u.id
		LEFT JOIN thread_state ON thread_state.root_message_id = m.id
		WHERE messages_fts MATCH ?
		ORDER BY `+outerOrderBy, args...)
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

func sqliteSearchScope(ctx context.Context, tx *sql.Tx, req store.SearchPageRequest, role string) (string, []any, error) {
	switch {
	case req.ChannelID != "":
		var channelName string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM channels WHERE id = ? AND workspace_id = ?`, req.ChannelID, req.WorkspaceID).Scan(&channelName); err != nil {
			return "", nil, err
		}
		if role == store.WorkspaceRoleGuest && channelName != store.GuestChannelName {
			return "", nil, store.ErrModerationRestricted
		}
		return "AND m.channel_id = ? AND m.direct_conversation_id IS NULL", []any{req.ChannelID}, nil
	case req.DirectConversationID != "":
		var workspaceID string
		if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM direct_conversations WHERE id = ?`, req.DirectConversationID).Scan(&workspaceID); err != nil {
			return "", nil, err
		}
		if workspaceID != req.WorkspaceID {
			return "", nil, fmt.Errorf("%w: direct conversation is not in workspace", store.ErrInvalidSearch)
		}
		if err := requireDirectAccessTx(ctx, tx, req.DirectConversationID, req.UserID); err != nil {
			return "", nil, err
		}
		return `AND m.direct_conversation_id = ?
			AND EXISTS (
				SELECT 1
				FROM direct_conversation_members dcm
				WHERE dcm.conversation_id = m.direct_conversation_id
				  AND dcm.user_id = ?
			)`, []any{req.DirectConversationID, req.UserID}, nil
	default:
		if role == store.WorkspaceRoleGuest {
			var guestChannelID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE workspace_id = ? AND name = ?`, req.WorkspaceID, store.GuestChannelName).Scan(&guestChannelID); err != nil {
				return "", nil, err
			}
			return "AND m.direct_conversation_id IS NULL AND m.channel_id = ?", []any{guestChannelID}, nil
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
