package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sql.DB
	q  *storedb.Queries
}

func Open(dbURL string) (*Store, error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, q: storedb.New(db)}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		var applied string
		err := s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = $1`, name).Scan(&applied)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES ($1, $2)`, name, now()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return s.backfillRouteIDsOnce(ctx)
}

func (s *Store) EnsureBootstrap(ctx context.Context, name, email string) (store.User, error) {
	user, err := s.FirstUser(ctx)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return store.User{}, err
	}
	user, err = s.CreateUser(ctx, store.CreateUserInput{DisplayName: name, Email: email})
	if err != nil {
		return store.User{}, err
	}
	ws, err := s.CreateWorkspace(ctx, store.CreateWorkspaceInput{Name: "ClickClack", Slug: "clickclack"}, user.ID)
	if err != nil {
		return store.User{}, err
	}
	_, _, err = s.CreateChannel(ctx, store.CreateChannelInput{WorkspaceID: ws.ID, Name: "general", Kind: "public", UserID: user.ID})
	return user, err
}

func (s *Store) CreateUser(ctx context.Context, input store.CreateUserInput) (store.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, err
	}
	defer tx.Rollback()
	user := store.User{
		ID:          newID("usr"),
		Kind:        "human",
		DisplayName: strings.TrimSpace(input.DisplayName),
		Handle:      "",
		AvatarURL:   "",
		CreatedAt:   now(),
	}
	if user.DisplayName == "" {
		user.DisplayName = "Local User"
	}
	qtx := s.q.WithTx(tx)
	if err := qtx.InsertHumanUser(ctx, storedb.InsertHumanUserParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		AvatarUrl:   user.AvatarURL,
		CreatedAt:   user.CreatedAt,
	}); err != nil {
		return store.User{}, err
	}
	if input.Email != "" {
		if err := qtx.InsertIdentity(ctx, storedb.InsertIdentityParams{
			ID:              newID("idn"),
			UserID:          user.ID,
			Provider:        "local",
			ProviderSubject: input.Email,
			Email:           input.Email,
			CreatedAt:       user.CreatedAt,
		}); err != nil {
			return store.User{}, err
		}
	}
	return user, tx.Commit()
}

func (s *Store) FirstUser(ctx context.Context) (store.User, error) {
	row, err := s.q.FirstUser(ctx)
	if err != nil {
		return store.User{}, err
	}
	return s.hydrateUserNotificationSettings(ctx, storeUserFromFirstUser(row))
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	row, err := s.q.GetUser(ctx, id)
	if err != nil {
		return store.User{}, err
	}
	return s.hydrateUserNotificationSettings(ctx, storeUserFromGetUser(row))
}

func (s *Store) UpdateUserProfile(ctx context.Context, input store.UpdateUserProfileInput) (store.User, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return store.User{}, errors.New("display_name is required")
	}
	if len(displayName) > 80 {
		return store.User{}, errors.New("display_name is too long")
	}
	handle, err := normalizeHandle(input.Handle)
	if err != nil {
		return store.User{}, err
	}
	avatarURL, err := normalizeAvatarURL(input.AvatarURL)
	if err != nil {
		return store.User{}, err
	}
	if err := s.q.UpdateUserProfile(ctx, storedb.UpdateUserProfileParams{
		DisplayName: displayName,
		Handle:      handle,
		AvatarUrl:   avatarURL,
		ID:          input.UserID,
	}); err != nil {
		if strings.Contains(err.Error(), "idx_users_handle") || strings.Contains(err.Error(), "users.handle") {
			return store.User{}, errors.New("handle is already taken")
		}
		return store.User{}, err
	}
	return s.GetUser(ctx, input.UserID)
}

func (s *Store) ListWorkspaces(ctx context.Context, userID string) ([]store.Workspace, error) {
	rows, err := s.q.ListWorkspaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Workspace, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeWorkspaceFromListWorkspaces(row))
	}
	return out, nil
}

func (s *Store) CreateWorkspace(ctx context.Context, input store.CreateWorkspaceInput, ownerID string) (store.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Workspace{}, err
	}
	defer tx.Rollback()
	w := store.Workspace{ID: newID("wsp"), Name: strings.TrimSpace(input.Name), Slug: slug(input.Slug), CreatedAt: now()}
	if w.Name == "" {
		w.Name = "Untitled"
	}
	if w.Slug == "" {
		w.Slug = slug(w.Name)
	}
	qtx := s.q.WithTx(tx)
	inserted := false
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		routeID, err := newRouteID('T')
		if err != nil {
			return store.Workspace{}, err
		}
		w.RouteID = routeID
		if err := qtx.InsertWorkspace(ctx, storedb.InsertWorkspaceParams{
			ID:        w.ID,
			RouteID:   sqlText(w.RouteID),
			Name:      w.Name,
			Slug:      w.Slug,
			CreatedAt: w.CreatedAt,
		}); err != nil {
			if isRouteIDConflict(err) {
				continue
			}
			return store.Workspace{}, err
		}
		inserted = true
		break
	}
	if !inserted {
		return store.Workspace{}, errors.New("could not create workspace route_id after collision retries")
	}
	if err := qtx.InsertWorkspaceMember(ctx, storedb.InsertWorkspaceMemberParams{
		WorkspaceID: w.ID,
		UserID:      ownerID,
		Role:        "owner",
		CreatedAt:   w.CreatedAt,
	}); err != nil {
		return store.Workspace{}, err
	}
	return w, tx.Commit()
}

func (s *Store) GetWorkspace(ctx context.Context, workspaceID, userID string) (store.Workspace, error) {
	row, err := s.q.GetWorkspace(ctx, storedb.GetWorkspaceParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		return store.Workspace{}, err
	}
	return storeWorkspaceFromGetWorkspace(row), nil
}

func (s *Store) ListChannels(ctx context.Context, workspaceID, userID string) ([]store.Channel, error) {
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListChannels(ctx, storedb.ListChannelsParams{ReaderUserID: userID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	out := make([]store.Channel, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeChannelFromListChannels(row))
	}
	return out, nil
}

func (s *Store) GetChannel(ctx context.Context, channelID, userID string) (store.Channel, error) {
	row, err := s.q.GetChannel(ctx, channelID)
	if err != nil {
		return store.Channel{}, err
	}
	channel := storeChannelFromGetChannel(row)
	if err := s.requireMembership(ctx, channel.WorkspaceID, userID); err != nil {
		return store.Channel{}, err
	}
	return channel, nil
}

func (s *Store) CreateChannel(ctx context.Context, input store.CreateChannelInput) (store.Channel, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Channel{}, store.Event{}, err
	}
	defer tx.Rollback()
	if err := requireMembershipTx(ctx, tx, input.WorkspaceID, input.UserID); err != nil {
		return store.Channel{}, store.Event{}, err
	}
	ch := store.Channel{ID: newID("chn"), WorkspaceID: input.WorkspaceID, Name: slug(input.Name), Kind: input.Kind, CreatedAt: now()}
	if ch.Name == "" {
		ch.Name = "general"
	}
	if ch.Kind == "" {
		ch.Kind = "public"
	}
	inserted := false
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		routeID, err := newRouteID('C')
		if err != nil {
			return store.Channel{}, store.Event{}, err
		}
		ch.RouteID = routeID
		if err := s.q.WithTx(tx).InsertChannel(ctx, storedb.InsertChannelParams{
			ID:          ch.ID,
			RouteID:     sqlText(ch.RouteID),
			WorkspaceID: ch.WorkspaceID,
			Name:        ch.Name,
			Kind:        ch.Kind,
			CreatedAt:   ch.CreatedAt,
		}); err != nil {
			if isRouteIDConflict(err) {
				continue
			}
			return store.Channel{}, store.Event{}, err
		}
		inserted = true
		break
	}
	if !inserted {
		return store.Channel{}, store.Event{}, errors.New("could not create channel route_id after collision retries")
	}
	event, err := insertEvent(ctx, tx, ch.WorkspaceID, ch.ID, "channel.created", nil, map[string]string{"channel_id": ch.ID})
	if err != nil {
		return store.Channel{}, store.Event{}, err
	}
	return ch, event, tx.Commit()
}

func (s *Store) ListMessages(ctx context.Context, channelID, userID string, page store.MessagePageRequest) (store.MessagePage, error) {
	workspaceID, err := s.q.GetChannelWorkspace(ctx, channelID)
	if err != nil {
		return store.MessagePage{}, err
	}
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return store.MessagePage{}, err
	}
	return s.listMessagePage(ctx, messagePageScope{
		where: "m.channel_id = $1 AND m.parent_message_id IS NULL",
		args:  []any{channelID},
	}, page)
}

func (s *Store) GetMessage(ctx context.Context, messageID, userID string) (store.Message, error) {
	message, err := getMessage(ctx, s.db, messageID)
	if err != nil {
		return store.Message{}, err
	}
	if err := s.requireMessageAccess(ctx, message, userID); err != nil {
		return store.Message{}, err
	}
	messages, err := s.hydrateAttachments(ctx, []store.Message{message})
	if err != nil {
		return store.Message{}, err
	}
	return messages[0], nil
}

func (s *Store) requireMessageAccess(ctx context.Context, message store.Message, userID string) error {
	if message.DirectConversationID != "" {
		return s.requireDirectMembership(ctx, message.DirectConversationID, userID)
	}
	return s.requireMembership(ctx, message.WorkspaceID, userID)
}

func requireMessageAccessTx(ctx context.Context, tx *sql.Tx, message store.Message, userID string) error {
	if message.DirectConversationID != "" {
		return requireDirectMembershipTx(ctx, tx, message.DirectConversationID, userID)
	}
	return requireMembershipTx(ctx, tx, message.WorkspaceID, userID)
}

func (s *Store) CreateMessage(ctx context.Context, input store.CreateMessageInput) (store.Message, store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	workspaceID, err := qtx.GetChannelWorkspace(ctx, input.ChannelID)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, input.AuthorID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	if err := lockMessageSequenceTx(ctx, tx, "channel", input.ChannelID); err != nil {
		return store.Message{}, store.Event{}, err
	}
	seq, err := qtx.ChannelNextSeq(ctx, input.ChannelID)
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
		if existing.ChannelID != input.ChannelID || existing.DirectConversationID != "" || existing.ParentMessageID != nil || existing.Body != body || !sameQuotedMessageID(existing, quotedID) {
			return store.Message{}, store.Event{}, store.ErrClientNonceConflict
		}
		return existing, store.Event{}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Message{}, store.Event{}, err
	}
	if quotedID != "" {
		snap, authorID, err := resolveQuoteRefTx(ctx, tx, quotedID, quoteScope{kind: "channel", channelID: input.ChannelID})
		if err != nil {
			return store.Message{}, store.Event{}, err
		}
		quotedSnapshot = snap
		quotedAuthorID = authorID
	}
	if err := qtx.InsertChannelMessage(ctx, storedb.InsertChannelMessageParams{
		ID:                 id,
		WorkspaceID:        workspaceID,
		ChannelID:          sqlText(input.ChannelID),
		AuthorID:           input.AuthorID,
		ThreadRootID:       id,
		ChannelSeq:         sqlInt64(seq),
		Body:               body,
		CreatedAt:          createdAt,
		QuotedMessageID:    sqlOptionalText(quotedID),
		QuotedBodySnapshot: quotedSnapshot,
		QuotedAuthorID:     sqlOptionalText(quotedAuthorID),
		ClientNonce:        nonce,
	}); err != nil {
		if existing, lookupErr := getMessageByClientNonceTx(ctx, tx, input.AuthorID, nonce); lookupErr == nil {
			if existing.ChannelID == input.ChannelID && existing.DirectConversationID == "" && existing.ParentMessageID == nil && existing.Body == body && sameQuotedMessageID(existing, quotedID) {
				return existing, store.Event{}, nil
			}
			return store.Message{}, store.Event{}, store.ErrClientNonceConflict
		}
		return store.Message{}, store.Event{}, err
	}
	if err := qtx.InsertThreadState(ctx, id); err != nil {
		return store.Message{}, store.Event{}, err
	}
	event, err := insertEvent(ctx, tx, workspaceID, input.ChannelID, "message.created", &seq, eventPayload(map[string]string{"message_id": id, "author_id": input.AuthorID}, nonce))
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	msg, err := getMessageTx(ctx, tx, id)
	if err != nil {
		return store.Message{}, store.Event{}, err
	}
	return msg, event, tx.Commit()
}

func (s *Store) GetThread(ctx context.Context, rootMessageID, userID string, limit int) (store.Message, []store.Message, store.ThreadState, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	root, err := getMessage(ctx, s.db, rootMessageID)
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	if root.ParentMessageID != nil {
		return store.Message{}, nil, store.ThreadState{}, errors.New("thread root must be a root message")
	}
	if err := s.requireMessageAccess(ctx, root, userID); err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	root, err = s.EnsureThreadRouteID(ctx, userID, root.ID)
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	roots, err := s.hydrateAttachments(ctx, []store.Message{root})
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	root = roots[0]
	rows, err := s.db.QueryContext(ctx, messageSelect()+`
		WHERE m.thread_root_id = $1 AND m.parent_message_id = $2
		ORDER BY m.thread_seq
		LIMIT $3`, rootMessageID, rootMessageID, limit)
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	defer rows.Close()
	replies, err := scanMessages(rows)
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	replies, err = s.hydrateAttachments(ctx, replies)
	if err != nil {
		return store.Message{}, nil, store.ThreadState{}, err
	}
	state, err := getThreadState(ctx, s.db, rootMessageID)
	return root, replies, state, err
}

func (s *Store) CreateThreadReply(ctx context.Context, input store.CreateThreadReplyInput) (store.Message, store.ThreadState, []store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	root, err := getMessageTx(ctx, tx, input.RootMessageID)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	if root.ParentMessageID != nil {
		return store.Message{}, store.ThreadState{}, nil, errors.New("nested thread replies are not supported")
	}
	if err := requireMessageAccessTx(ctx, tx, root, input.AuthorID); err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	root, err = ensureThreadRouteIDTx(ctx, tx, root)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	if err := lockMessageSequenceTx(ctx, tx, "thread", root.ID); err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	seq, err := qtx.ThreadNextSeq(ctx, storedb.ThreadNextSeqParams{ThreadRootID: root.ID, ParentMessageID: sqlText(root.ID)})
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	id := newID("msg")
	createdAt := now()
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return store.Message{}, store.ThreadState{}, nil, errors.New("reply body is required")
	}
	nonce, err := normalizeClientNonce(input.Nonce)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	var quotedID, quotedAuthorID, quotedSnapshot string
	if input.QuotedMessageID != nil {
		quotedID = strings.TrimSpace(*input.QuotedMessageID)
	}
	if existing, err := getMessageByClientNonceTx(ctx, tx, input.AuthorID, nonce); err == nil {
		if existing.ThreadRootID != root.ID || existing.ParentMessageID == nil || *existing.ParentMessageID != root.ID || existing.Body != body || !sameQuotedMessageID(existing, quotedID) {
			return store.Message{}, store.ThreadState{}, nil, store.ErrClientNonceConflict
		}
		stateRow, err := qtx.GetThreadState(ctx, root.ID)
		if err != nil {
			return store.Message{}, store.ThreadState{}, nil, err
		}
		return existing, storeThreadStateFromDB(stateRow), nil, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	if quotedID != "" {
		snap, authorID, err := resolveQuoteRefTx(ctx, tx, quotedID, quoteScope{kind: "thread", threadRootID: root.ID})
		if err != nil {
			return store.Message{}, store.ThreadState{}, nil, err
		}
		quotedSnapshot = snap
		quotedAuthorID = authorID
	}
	var channelID sql.NullString
	var directConversationID sql.NullString
	if root.DirectConversationID != "" {
		directConversationID = sqlText(root.DirectConversationID)
	} else {
		channelID = sqlText(root.ChannelID)
	}
	if err := qtx.InsertThreadReply(ctx, storedb.InsertThreadReplyParams{
		ID:                   id,
		WorkspaceID:          root.WorkspaceID,
		ChannelID:            channelID,
		DirectConversationID: directConversationID,
		AuthorID:             input.AuthorID,
		ParentMessageID:      sqlText(root.ID),
		ThreadRootID:         root.ID,
		ThreadSeq:            sqlInt64(seq),
		Body:                 body,
		CreatedAt:            createdAt,
		QuotedMessageID:      sqlOptionalText(quotedID),
		QuotedBodySnapshot:   quotedSnapshot,
		QuotedAuthorID:       sqlOptionalText(quotedAuthorID),
		ClientNonce:          nonce,
	}); err != nil {
		if existing, lookupErr := getMessageByClientNonceTx(ctx, tx, input.AuthorID, nonce); lookupErr == nil {
			if existing.ThreadRootID == root.ID && existing.ParentMessageID != nil && *existing.ParentMessageID == root.ID && existing.Body == body && sameQuotedMessageID(existing, quotedID) {
				stateRow, stateErr := qtx.GetThreadState(ctx, root.ID)
				if stateErr != nil {
					return store.Message{}, store.ThreadState{}, nil, stateErr
				}
				return existing, storeThreadStateFromDB(stateRow), nil, nil
			}
			return store.Message{}, store.ThreadState{}, nil, store.ErrClientNonceConflict
		}
		return store.Message{}, store.ThreadState{}, nil, err
	}
	state, err := updateThreadState(ctx, tx, root.ID, input.AuthorID, createdAt)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	replyPayload := eventPayload(map[string]string{"message_id": id, "root_message_id": root.ID}, nonce)
	statePayload := map[string]string{"root_message_id": root.ID}
	var recipients []string
	if root.DirectConversationID != "" {
		replyPayload["direct_conversation_id"] = root.DirectConversationID
		statePayload["direct_conversation_id"] = root.DirectConversationID
		recipients, err = directConversationMemberIDsTx(ctx, tx, root.DirectConversationID)
		if err != nil {
			return store.Message{}, store.ThreadState{}, nil, err
		}
	}
	replyEvent, err := insertEventWithRecipients(ctx, tx, root.WorkspaceID, root.ChannelID, "thread.reply_created", nil, replyPayload, recipients)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	stateEvent, err := insertEventWithRecipients(ctx, tx, root.WorkspaceID, root.ChannelID, "thread.state_updated", nil, statePayload, recipients)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	msg, err := getMessageTx(ctx, tx, id)
	if err != nil {
		return store.Message{}, store.ThreadState{}, nil, err
	}
	return msg, state, []store.Event{replyEvent, stateEvent}, tx.Commit()
}

func (s *Store) AddReaction(ctx context.Context, input store.CreateReactionInput) (store.Event, error) {
	return s.reaction(ctx, input, true)
}

func (s *Store) RemoveReaction(ctx context.Context, input store.CreateReactionInput) (store.Event, error) {
	return s.reaction(ctx, input, false)
}

func (s *Store) ListEventsAfter(ctx context.Context, workspaceID, userID, cursor string, limit int) ([]store.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if err := s.requireMembership(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListEventsAfter(ctx, storedb.ListEventsAfterParams{
		WorkspaceID: workspaceID,
		Cursor:      cursor,
		UserID:      userID,
		LimitCount:  int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]store.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, storeEventFromListEventsAfter(row))
	}
	return out, nil
}

func (s *Store) reaction(ctx context.Context, input store.CreateReactionInput, add bool) (store.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Event{}, err
	}
	defer tx.Rollback()
	msg, err := getMessageTx(ctx, tx, input.MessageID)
	if err != nil {
		return store.Event{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, msg, input.UserID); err != nil {
		return store.Event{}, err
	}
	qtx := s.q.WithTx(tx)
	var affected int64
	if add {
		affected, err = qtx.AddReaction(ctx, storedb.AddReactionParams{MessageID: input.MessageID, UserID: input.UserID, Emoji: input.Emoji, CreatedAt: now()})
	} else {
		affected, err = qtx.RemoveReaction(ctx, storedb.RemoveReactionParams{MessageID: input.MessageID, UserID: input.UserID, Emoji: input.Emoji})
	}
	if err != nil {
		return store.Event{}, err
	}
	if affected == 0 {
		return store.Event{}, tx.Commit()
	}
	eventType := "reaction.added"
	if !add {
		eventType = "reaction.removed"
	}
	payload := map[string]string{"message_id": input.MessageID, "emoji": input.Emoji}
	if msg.DirectConversationID != "" {
		payload["direct_conversation_id"] = msg.DirectConversationID
	}
	recipients, err := eventRecipientsForMessageTx(ctx, tx, msg)
	if err != nil {
		return store.Event{}, err
	}
	event, err := insertEventWithRecipients(ctx, tx, msg.WorkspaceID, msg.ChannelID, eventType, msg.ChannelSeq, payload, recipients)
	if err != nil {
		return store.Event{}, err
	}
	return event, tx.Commit()
}

func (s *Store) requireMembership(ctx context.Context, workspaceID, userID string) error {
	_, err := s.q.RequireMembership(ctx, storedb.RequireMembershipParams{WorkspaceID: workspaceID, UserID: userID})
	return err
}

func requireMembershipTx(ctx context.Context, tx *sql.Tx, workspaceID, userID string) error {
	_, err := storedb.New(tx).RequireMembership(ctx, storedb.RequireMembershipParams{WorkspaceID: workspaceID, UserID: userID})
	return err
}
