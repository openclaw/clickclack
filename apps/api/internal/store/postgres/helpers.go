package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"github.com/openclaw/clickclack/apps/api/internal/requestmeta"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (store.User, error) {
	var user store.User
	var owner sql.NullString
	if err := row.Scan(&user.ID, &user.Kind, &owner, &user.DisplayName, &user.Handle, &user.AvatarURL, &user.CreatedAt); err != nil {
		return store.User{}, err
	}
	user.OwnerUserID = stringFromNull(owner)
	return user, nil
}

var (
	idMu      sync.Mutex
	idEntropy = ulid.Monotonic(rand.Reader, 0)
)

func getMessage(ctx context.Context, db *sql.DB, id string) (store.Message, error) {
	return scanMessage(db.QueryRowContext(ctx, messageSelect()+` WHERE m.id = $1`, id))
}

func getMessageTx(ctx context.Context, tx *sql.Tx, id string) (store.Message, error) {
	return scanMessage(tx.QueryRowContext(ctx, messageSelect()+` WHERE m.id = $1`, id))
}

func messageSelect() string {
	return `SELECT m.id, COALESCE(m.route_id, ''), m.workspace_id, COALESCE(m.channel_id, ''), COALESCE(m.direct_conversation_id, ''), m.author_id, m.parent_message_id, m.thread_root_id, COALESCE(m.topic_id, ''), m.channel_seq, m.thread_seq,
		       m.body, m.body_format, m.created_at, m.edited_at, m.deleted_at,
		       u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at,
		       m.quoted_message_id, m.quoted_body_snapshot, m.quoted_author_id,
		       qu.id, qu.kind, qu.owner_user_id, qu.display_name, qu.handle, qu.avatar_url, qu.created_at,
		       m.client_nonce, m.kind, COALESCE(m.turn_id, '')
		FROM messages m
		JOIN users u ON u.id = m.author_id
		LEFT JOIN users qu ON qu.id = m.quoted_author_id`
}

func scanMessage(row scanner) (store.Message, error) {
	var m store.Message
	var parent, edited, deleted sql.NullString
	var channelSeq, threadSeq sql.NullInt64
	var author store.User
	var quotedMessageID, quotedAuthorID sql.NullString
	var authorOwnerID sql.NullString
	var quAuthorID, quKind, quOwnerID, quDisplayName, quHandle, quAvatarURL, quCreatedAt sql.NullString
	var nonce string
	err := row.Scan(
		&m.ID, &m.RouteID, &m.WorkspaceID, &m.ChannelID, &m.DirectConversationID, &m.AuthorID, &parent, &m.ThreadRootID, &m.TopicID, &channelSeq, &threadSeq,
		&m.Body, &m.BodyFormat, &m.CreatedAt, &edited, &deleted,
		&author.ID, &author.Kind, &authorOwnerID, &author.DisplayName, &author.Handle, &author.AvatarURL, &author.CreatedAt,
		&quotedMessageID, &m.QuotedBodySnapshot, &quotedAuthorID,
		&quAuthorID, &quKind, &quOwnerID, &quDisplayName, &quHandle, &quAvatarURL, &quCreatedAt,
		&nonce, &m.Kind, &m.TurnID,
	)
	if err != nil {
		return store.Message{}, err
	}
	if parent.Valid {
		m.ParentMessageID = &parent.String
	}
	if channelSeq.Valid {
		m.ChannelSeq = &channelSeq.Int64
	}
	if threadSeq.Valid {
		m.ThreadSeq = &threadSeq.Int64
	}
	if edited.Valid {
		m.EditedAt = &edited.String
	}
	if deleted.Valid {
		m.DeletedAt = &deleted.String
	}
	if authorOwnerID.Valid {
		author.OwnerUserID = authorOwnerID.String
	}
	m.Author = &author
	if quotedMessageID.Valid {
		m.QuotedMessageID = &quotedMessageID.String
	}
	if quotedAuthorID.Valid {
		m.QuotedAuthorID = &quotedAuthorID.String
	}
	if quAuthorID.Valid {
		m.QuotedAuthor = &store.User{
			ID:          quAuthorID.String,
			Kind:        quKind.String,
			OwnerUserID: quOwnerID.String,
			DisplayName: quDisplayName.String,
			Handle:      quHandle.String,
			AvatarURL:   quAvatarURL.String,
			CreatedAt:   quCreatedAt.String,
		}
	}
	m.Nonce = nonce
	return m, nil
}

func normalizeClientNonce(value string) (string, error) {
	nonce := strings.TrimSpace(value)
	if !utf8.ValidString(nonce) {
		return "", errors.New("nonce must be valid UTF-8")
	}
	if strings.IndexByte(nonce, 0) >= 0 {
		return "", errors.New("nonce must not contain NUL")
	}
	if utf8.RuneCountInString(nonce) > 128 {
		return "", errors.New("nonce is too long")
	}
	return nonce, nil
}

func getMessageByClientNonceTx(ctx context.Context, tx *sql.Tx, authorID, nonce string) (store.Message, error) {
	if nonce == "" {
		return store.Message{}, sql.ErrNoRows
	}
	return scanMessage(tx.QueryRowContext(ctx, messageSelect()+` WHERE m.author_id = $1 AND m.client_nonce = $2`, authorID, nonce))
}

func sameQuotedMessageID(message store.Message, quotedID string) bool {
	if quotedID == "" {
		return message.QuotedMessageID == nil || *message.QuotedMessageID == ""
	}
	return message.QuotedMessageID != nil && *message.QuotedMessageID == quotedID
}

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

func normalizeHandle(value string) (string, error) {
	handle := strings.ToLower(strings.TrimSpace(value))
	handle = strings.TrimPrefix(handle, "@")
	if handle == "" {
		return "", nil
	}
	if !handlePattern.MatchString(handle) {
		return "", errors.New("handle must be 2-32 chars using letters, numbers, underscores, or dashes")
	}
	return handle, nil
}

func normalizeAvatarURL(value string) (string, error) {
	avatarURL := strings.TrimSpace(value)
	if avatarURL == "" {
		return "", nil
	}
	if len(avatarURL) > 500 {
		return "", errors.New("avatar_url is too long")
	}
	parsed, err := url.Parse(avatarURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("avatar_url must be an http or https URL")
	}
	return avatarURL, nil
}

func scanMessages(rows *sql.Rows) ([]store.Message, error) {
	out := []store.Message{}
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func getThreadState(ctx context.Context, db *sql.DB, rootID string) (store.ThreadState, error) {
	row, err := storedb.New(db).GetThreadState(ctx, rootID)
	if err != nil {
		return store.ThreadState{}, err
	}
	return storeThreadStateFromDB(row), nil
}

func lockMessageSequenceTx(ctx context.Context, tx *sql.Tx, kind, id string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, "clickclack.seq."+kind, id)
	return err
}

func updateThreadState(ctx context.Context, tx *sql.Tx, rootID, authorID, createdAt string) (store.ThreadState, error) {
	q := storedb.New(tx)
	row, err := q.GetThreadState(ctx, rootID)
	if err != nil {
		return store.ThreadState{}, err
	}
	state := storeThreadStateFromDB(row)
	ids := append([]string{authorID}, state.LastReplyAuthorIDs...)
	seen := map[string]bool{}
	compact := make([]string, 0, 3)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		compact = append(compact, id)
		if len(compact) == 3 {
			break
		}
	}
	body, _ := json.Marshal(compact)
	if err := q.UpdateThreadState(ctx, storedb.UpdateThreadStateParams{
		LastReplyAt:            sqlText(createdAt),
		LastReplyAuthorIdsJson: string(body),
		RootMessageID:          rootID,
	}); err != nil {
		return store.ThreadState{}, err
	}
	row, err = q.GetThreadState(ctx, rootID)
	if err != nil {
		return store.ThreadState{}, err
	}
	return storeThreadStateFromDB(row), nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, workspaceID, channelID, eventType string, seq *int64, payload any) (store.Event, error) {
	return insertEventWithRecipients(ctx, tx, workspaceID, channelID, eventType, seq, payload, nil)
}

func insertEventWithRecipients(ctx context.Context, tx *sql.Tx, workspaceID, channelID, eventType string, seq *int64, payload any, recipientUserIDs []string) (store.Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return store.Event{}, err
	}
	recipients := compactStrings(recipientUserIDs)
	event := store.Event{
		ID:          newID("evt"),
		Cursor:      newID("cur"),
		Type:        eventType,
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
		Seq:         seq,
		CreatedAt:   now(),
		PayloadJSON: string(payloadJSON),
		Payload:     payload,
	}
	isPrivate := 0
	if len(recipients) > 0 {
		isPrivate = 1
	}
	q := storedb.New(tx)
	if err := q.InsertEvent(ctx, storedb.InsertEventParams{
		ID:          event.ID,
		Cursor:      event.Cursor,
		WorkspaceID: event.WorkspaceID,
		ChannelID:   sqlOptionalText(event.ChannelID),
		Type:        event.Type,
		Seq:         nullInt64FromPtr(event.Seq),
		PayloadJson: event.PayloadJSON,
		CreatedAt:   event.CreatedAt,
		IsPrivate:   int64(isPrivate),
	}); err != nil {
		return store.Event{}, err
	}
	for _, userID := range recipients {
		if err := q.InsertEventRecipient(ctx, storedb.InsertEventRecipientParams{EventID: event.ID, UserID: userID}); err != nil {
			return store.Event{}, err
		}
		event.RecipientUserIDs = append(event.RecipientUserIDs, userID)
	}
	return event, nil
}

// eventPayload adds optional client and request correlation metadata to durable
// message creation events without copying message content.
func eventPayload(ctx context.Context, base map[string]string, nonce string) map[string]string {
	correlationID := requestmeta.CorrelationID(ctx)
	if nonce == "" && correlationID == "" {
		return base
	}
	out := make(map[string]string, len(base)+2)
	for k, v := range base {
		out[k] = v
	}
	if nonce != "" {
		out["nonce"] = nonce
	}
	if correlationID != "" {
		out["correlation_id"] = correlationID
	}
	return out
}

func newID(prefix string) string {
	idMu.Lock()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), idEntropy)
	idMu.Unlock()
	return prefix + "_" + strings.ToLower(id.String())
}

const routeIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newRouteID(prefix byte) (string, error) {
	const routeIDRandomBytes = 10
	var raw [routeIDRandomBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	var out [17]byte
	out[0] = prefix
	bitBuffer := 0
	bits := 0
	pos := 1
	for _, b := range raw {
		bitBuffer = (bitBuffer << 8) | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[pos] = routeIDAlphabet[(bitBuffer>>bits)&31]
			pos++
			if bits > 0 {
				bitBuffer &= (1 << bits) - 1
			} else {
				bitBuffer = 0
			}
		}
	}
	if pos != len(out) {
		return "", fmt.Errorf("route id encoder produced %d characters", pos)
	}
	return string(out[:]), nil
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugRE.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func isReservedWorkspaceSlug(value string) bool {
	return value == "clickclack" || value == "guests"
}
