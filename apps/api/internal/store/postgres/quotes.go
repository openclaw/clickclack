package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// maxQuoteSnapshotChars caps the size of a quoted-body snapshot stored on a
// message row. Snapshots are intentionally short — they are a UI hint, not a
// substitute for the live message — so a tweet-length cap keeps rows compact
// and renders predictably.
const maxQuoteSnapshotChars = 280

// quoteScope describes the conversation the new message will live in. Exactly
// one of channelID / directConversationID is required when scope is "channel"
// or "dm"; threadRootID is required when scope is "thread".
type quoteScope struct {
	kind                 string // "channel" | "dm" | "thread"
	channelID            string
	directConversationID string
	threadRootID         string
}

// resolveQuoteRefTx validates that quotedID lives in the given scope and
// returns the snapshot fields to persist. quotedID must be non-empty; callers
// should skip this when the user did not provide a quote reference.
//
// Returns store.ErrQuotedMessageOutOfScope when the quoted message is not in
// the same channel/DM/thread, or has been soft-deleted.
func resolveQuoteRefTx(ctx context.Context, tx *sql.Tx, quotedID string, scope quoteScope) (snapshot string, authorID string, err error) {
	quoted, err := getMessageTx(ctx, tx, quotedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", store.ErrQuotedMessageOutOfScope
		}
		return "", "", err
	}
	if quoted.DeletedAt != nil {
		return "", "", store.ErrQuotedMessageOutOfScope
	}
	switch scope.kind {
	case "channel":
		if quoted.ChannelID == "" || quoted.ChannelID != scope.channelID || quoted.ParentMessageID != nil {
			return "", "", store.ErrQuotedMessageOutOfScope
		}
	case "dm":
		if quoted.DirectConversationID == "" || quoted.DirectConversationID != scope.directConversationID || quoted.ParentMessageID != nil {
			return "", "", store.ErrQuotedMessageOutOfScope
		}
	case "thread":
		if quoted.ThreadRootID != scope.threadRootID {
			return "", "", store.ErrQuotedMessageOutOfScope
		}
	default:
		return "", "", errors.New("invalid quote scope")
	}
	return truncateSnapshot(quoted.Body), quoted.AuthorID, nil
}

// truncateSnapshot trims surrounding whitespace and clips to
// maxQuoteSnapshotChars by rune count so multibyte characters aren't split.
func truncateSnapshot(body string) string {
	trimmed := strings.TrimSpace(body)
	runes := []rune(trimmed)
	if len(runes) <= maxQuoteSnapshotChars {
		return trimmed
	}
	return string(runes[:maxQuoteSnapshotChars])
}
