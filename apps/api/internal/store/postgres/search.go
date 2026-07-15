package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) SearchMessages(ctx context.Context, workspaceID, channelID, userID, query string, limit int) ([]store.SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	role, err := s.memberRole(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []store.SearchResult{}, nil
	}
	if channelID != "" {
		if err := s.requireGuestChannelAccess(ctx, workspaceID, channelID, userID); err != nil {
			return nil, err
		}
	}
	channelJoin := ""
	guestWhere := ""
	if role == store.WorkspaceRoleGuest {
		channelJoin = "JOIN channels c ON c.id = m.channel_id AND c.workspace_id = m.workspace_id"
		guestWhere = "AND c.name = 'guest'"
	}
	channelWhere := ""
	args := []any{workspaceID, query}
	if channelID != "" {
		channelWhere = " AND m.channel_id = $3"
		args = append(args, channelID)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.workspace_id, COALESCE(m.channel_id, ''), COALESCE(m.direct_conversation_id, ''), m.author_id, m.parent_message_id, m.thread_root_id, m.channel_seq, m.thread_seq,
		       m.body, m.body_format, m.created_at, m.edited_at, m.deleted_at,
		       u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at,
		       author_tombstone.former_handle, author_tombstone.deleted_at,
		       m.quoted_message_id, m.quoted_body_snapshot, m.quoted_author_id,
		       qu.id, qu.kind, qu.owner_user_id, qu.display_name, qu.handle, qu.avatar_url, qu.created_at,
		       quoted_tombstone.former_handle, quoted_tombstone.deleted_at,
		       ts_rank_cd(to_tsvector('simple', m.body), websearch_to_tsquery('simple', $2)) AS rank,
		       ts_headline('simple', m.body, websearch_to_tsquery('simple', $2),
		         'StartSel=`+string(store.SearchHighlightStart)+`, StopSel=`+string(store.SearchHighlightEnd)+`, MaxWords=24, MinWords=12, MaxFragments=2, FragmentDelimiter=…') AS snippet
		FROM messages m
		`+channelJoin+`
		JOIN users u ON u.id = m.author_id
		LEFT JOIN bot_tombstones author_tombstone ON author_tombstone.bot_user_id = u.id
		LEFT JOIN users qu ON qu.id = m.quoted_author_id
		LEFT JOIN bot_tombstones quoted_tombstone ON quoted_tombstone.bot_user_id = qu.id
		WHERE m.workspace_id = $1
		  AND to_tsvector('simple', m.body) @@ websearch_to_tsquery('simple', $2)
		  AND m.direct_conversation_id IS NULL
		  AND m.channel_id IS NOT NULL
		  AND m.deleted_at IS NULL
		  AND m.kind = 'message'
		  `+guestWhere+`
		  `+channelWhere+`
		ORDER BY rank DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.SearchResult{}
	for rows.Next() {
		msg, rank, markedSnippet, err := scanSearchMessage(rows)
		if err != nil {
			return nil, err
		}
		snippet, highlights := store.ParseSearchSnippet(markedSnippet)
		out = append(out, store.SearchResult{Message: msg, Rank: rank, Snippet: snippet, Highlights: highlights})
	}
	return out, rows.Err()
}

func scanSearchMessage(row scanner) (store.Message, float64, string, error) {
	var msg store.Message
	var parent, edited, deleted sql.NullString
	var channelSeq, threadSeq sql.NullInt64
	var author store.User
	var quotedMessageID, quotedAuthorID sql.NullString
	var authorOwnerID sql.NullString
	var quAuthorID, quKind, quOwnerID, quDisplayName, quHandle, quAvatarURL, quCreatedAt sql.NullString
	var authorFormerHandle, authorDeletedAt, quFormerHandle, quDeletedAt sql.NullString
	var rank float64
	var markedSnippet string
	err := row.Scan(
		&msg.ID, &msg.WorkspaceID, &msg.ChannelID, &msg.DirectConversationID, &msg.AuthorID, &parent, &msg.ThreadRootID, &channelSeq, &threadSeq,
		&msg.Body, &msg.BodyFormat, &msg.CreatedAt, &edited, &deleted,
		&author.ID, &author.Kind, &authorOwnerID, &author.DisplayName, &author.Handle, &author.AvatarURL, &author.CreatedAt,
		&authorFormerHandle, &authorDeletedAt,
		&quotedMessageID, &msg.QuotedBodySnapshot, &quotedAuthorID,
		&quAuthorID, &quKind, &quOwnerID, &quDisplayName, &quHandle, &quAvatarURL, &quCreatedAt,
		&quFormerHandle, &quDeletedAt,
		&rank, &markedSnippet,
	)
	if err != nil {
		return store.Message{}, 0, "", err
	}
	if parent.Valid {
		msg.ParentMessageID = &parent.String
	}
	if channelSeq.Valid {
		msg.ChannelSeq = &channelSeq.Int64
	}
	if threadSeq.Valid {
		msg.ThreadSeq = &threadSeq.Int64
	}
	if edited.Valid {
		msg.EditedAt = &edited.String
	}
	if deleted.Valid {
		msg.DeletedAt = &deleted.String
	}
	if authorOwnerID.Valid {
		author.OwnerUserID = authorOwnerID.String
	}
	if authorFormerHandle.Valid {
		author.FormerHandle = authorFormerHandle.String
	}
	if authorDeletedAt.Valid {
		author.DeletedAt = &authorDeletedAt.String
	}
	msg.Author = &author
	if quotedMessageID.Valid {
		msg.QuotedMessageID = &quotedMessageID.String
	}
	if quotedAuthorID.Valid {
		msg.QuotedAuthorID = &quotedAuthorID.String
	}
	if quAuthorID.Valid {
		msg.QuotedAuthor = &store.User{
			ID:          quAuthorID.String,
			Kind:        quKind.String,
			OwnerUserID: quOwnerID.String,
			DisplayName: quDisplayName.String,
			Handle:      quHandle.String,
			AvatarURL:   quAvatarURL.String,
			CreatedAt:   quCreatedAt.String,
		}
		if quFormerHandle.Valid {
			msg.QuotedAuthor.FormerHandle = quFormerHandle.String
		}
		if quDeletedAt.Valid {
			msg.QuotedAuthor.DeletedAt = &quDeletedAt.String
		}
	}
	return msg, rank, markedSnippet, nil
}
