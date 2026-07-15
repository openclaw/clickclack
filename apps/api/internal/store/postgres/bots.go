package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

var botScopeBundles = map[string][]string{
	"bot:read": {
		"workspaces:read",
		"channels:read",
		"messages:read",
		"threads:read",
		"dms:read",
		"realtime:read",
		"profile:read",
	},
	"bot:write": {
		"workspaces:read",
		"channels:read",
		"messages:read",
		"messages:write",
		"threads:read",
		"threads:write",
		"dms:read",
		"dms:write",
		"realtime:read",
		"uploads:write",
		"profile:read",
		store.BotCommandsWriteScope,
	},
	"bot:admin": {
		"workspaces:read",
		"channels:read",
		"channels:write",
		"messages:read",
		"messages:write",
		"threads:read",
		"threads:write",
		"dms:read",
		"dms:write",
		"realtime:read",
		"uploads:write",
		"profile:read",
		store.BotCommandsWriteScope,
	},
}

var botAllowedScopes = []string{
	"workspaces:read",
	"channels:read",
	"channels:write",
	"messages:read",
	"messages:write",
	"threads:read",
	"threads:write",
	"dms:read",
	"dms:write",
	"realtime:read",
	"uploads:write",
	"profile:read",
	store.BotCommandsWriteScope,
	// agent_activity:write is grantable but intentionally NOT part of any
	// bot:* bundle. Durable agent activity is a distinct authorization surface
	// and must be granted explicitly, so existing bot:* deployments gain no new
	// capability.
	store.AgentActivityWriteScope,
}

func (s *Store) CreateBot(ctx context.Context, input store.CreateBotInput) (store.User, store.BotToken, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return store.User{}, store.BotToken{}, errors.New("workspace is required")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return store.User{}, store.BotToken{}, errors.New("display_name is required")
	}
	if len(displayName) > 80 {
		return store.User{}, store.BotToken{}, errors.New("display_name is too long")
	}
	handle, err := normalizeHandle(input.Handle)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	avatarURL, err := normalizeAvatarURL(input.AvatarURL)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	scopes, err := normalizeBotScopes(input.Scopes)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	tokenName := strings.TrimSpace(input.TokenName)
	if tokenName == "" {
		tokenName = "default"
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	if createdBy == "" {
		return store.User{}, store.BotToken{}, errors.New("created_by is required")
	}
	ownerUserID := strings.TrimSpace(input.OwnerUserID)
	setupNonce, err := normalizeSetupNonce(input.SetupNonce)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if setupNonce != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, "clickclack.bot-token-setup."+createdBy, setupNonce); err != nil {
			return store.User{}, store.BotToken{}, err
		}
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.User{}, store.BotToken{}, err
	}
	if ownerUserID == "" {
		if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, createdBy); err != nil {
			return store.User{}, store.BotToken{}, err
		}
	} else {
		if createdBy != ownerUserID {
			return store.User{}, store.BotToken{}, store.ErrBotOwnerCreateRequired
		}
		ownerRow, err := qtx.GetUser(ctx, ownerUserID)
		if err != nil {
			return store.User{}, store.BotToken{}, err
		}
		owner := storeUserFromGetUser(ownerRow)
		if owner.Kind == "bot" {
			return store.User{}, store.BotToken{}, errors.New("bot owner must be a human")
		}
		if err := requireMembershipTx(ctx, tx, workspaceID, owner.ID); err != nil {
			return store.User{}, store.BotToken{}, errors.New("bot owner is not a workspace member")
		}
	}
	if setupNonce != "" {
		replayBot, replayToken, replayErr := getSetupBotTokenTx(ctx, tx, createdBy, setupNonce)
		if replayErr == nil {
			if replayToken.WorkspaceID != workspaceID ||
				replayToken.Name != tokenName ||
				!botSetupScopesMatch(input.Scopes, replayToken.Scopes, scopes) ||
				replayToken.RevokedAt != nil ||
				replayBot.OwnerUserID != ownerUserID ||
				replayBot.DisplayName != displayName ||
				replayBot.Handle != handle ||
				replayBot.AvatarURL != avatarURL {
				return store.User{}, store.BotToken{}, store.ErrSetupNonceConflict
			}
			replayToken, err = rotateSetupBotTokenTx(ctx, tx, replayToken)
			if err != nil {
				return store.User{}, store.BotToken{}, err
			}
			return replayBot, replayToken, tx.Commit()
		}
		if !errors.Is(replayErr, sql.ErrNoRows) {
			return store.User{}, store.BotToken{}, replayErr
		}
	}
	bot := store.User{
		ID:          newID("usr"),
		Kind:        "bot",
		OwnerUserID: ownerUserID,
		DisplayName: displayName,
		Handle:      handle,
		AvatarURL:   avatarURL,
		CreatedAt:   now(),
	}
	if err := qtx.InsertBotUser(ctx, storedb.InsertBotUserParams{
		ID:          bot.ID,
		OwnerUserID: sqlOptionalText(bot.OwnerUserID),
		DisplayName: bot.DisplayName,
		Handle:      bot.Handle,
		AvatarUrl:   bot.AvatarURL,
		CreatedAt:   bot.CreatedAt,
	}); err != nil {
		if strings.Contains(err.Error(), "idx_users_handle") || strings.Contains(err.Error(), "users.handle") {
			return store.User{}, store.BotToken{}, errors.New("handle is already taken")
		}
		return store.User{}, store.BotToken{}, err
	}
	if err := qtx.InsertWorkspaceMember(ctx, storedb.InsertWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		UserID:      bot.ID,
		Role:        "bot",
		CreatedAt:   bot.CreatedAt,
	}); err != nil {
		return store.User{}, store.BotToken{}, err
	}
	token := newID("ccb")
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	botToken := store.BotToken{
		ID:          newID("btok"),
		Token:       token,
		BotUserID:   bot.ID,
		WorkspaceID: workspaceID,
		OwnerUserID: bot.OwnerUserID,
		Name:        tokenName,
		Scopes:      scopes,
		CreatedBy:   createdBy,
		CreatedAt:   bot.CreatedAt,
	}
	if err := qtx.InsertBotToken(ctx, storedb.InsertBotTokenParams{
		ID:          botToken.ID,
		TokenHash:   hashBotToken(token),
		BotUserID:   botToken.BotUserID,
		WorkspaceID: botToken.WorkspaceID,
		OwnerUserID: sqlOptionalText(botToken.OwnerUserID),
		Name:        botToken.Name,
		ScopesJson:  string(scopesJSON),
		CreatedBy:   sqlOptionalText(botToken.CreatedBy),
		SetupNonce:  setupNonce,
		CreatedAt:   botToken.CreatedAt,
	}); err != nil {
		return store.User{}, store.BotToken{}, err
	}
	return bot, botToken, tx.Commit()
}

func (s *Store) GetBotTokenAuth(ctx context.Context, token string) (store.BotTokenAuth, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "ccb_") {
		return store.BotTokenAuth{}, sql.ErrNoRows
	}
	row, err := s.q.GetBotTokenAuth(ctx, hashBotToken(token))
	if err != nil {
		return store.BotTokenAuth{}, err
	}
	auth := storeBotTokenAuthFromDB(row)
	if err := json.Unmarshal([]byte(row.ScopesJson), &auth.Scopes); err != nil {
		return store.BotTokenAuth{}, err
	}
	if auth.User.OwnerUserID != "" {
		if err := s.requireMembership(ctx, auth.WorkspaceID, auth.User.OwnerUserID); err != nil {
			return store.BotTokenAuth{}, errors.New("bot owner is not a workspace member")
		}
	}
	if err := s.requireMembership(ctx, auth.WorkspaceID, auth.User.ID); err != nil {
		return store.BotTokenAuth{}, err
	}
	_ = s.q.TouchBotToken(ctx, storedb.TouchBotTokenParams{LastUsedAt: sqlText(now()), ID: auth.TokenID})
	return auth, nil
}

func (s *Store) ListBots(ctx context.Context, workspaceID, requesterID string) ([]store.BotWithTokens, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	botRows, err := s.q.ListWorkspaceBots(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	tokenRows, err := s.q.ListWorkspaceBotTokenMetadata(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	tokensByBot := make(map[string][]store.BotToken, len(botRows))
	for _, row := range tokenRows {
		var scopes []string
		if err := json.Unmarshal([]byte(row.ScopesJson), &scopes); err != nil {
			return nil, err
		}
		token := store.BotToken{
			ID:          row.ID,
			BotUserID:   row.BotUserID,
			WorkspaceID: row.WorkspaceID,
			OwnerUserID: row.OwnerUserID,
			Name:        row.Name,
			Scopes:      scopes,
			CreatedBy:   row.CreatedBy,
			CreatedAt:   row.CreatedAt,
		}
		if row.LastUsedAt != "" {
			lastUsedAt := row.LastUsedAt
			token.LastUsedAt = &lastUsedAt
		}
		if row.RevokedAt != "" {
			revokedAt := row.RevokedAt
			token.RevokedAt = &revokedAt
		}
		tokensByBot[row.BotUserID] = append(tokensByBot[row.BotUserID], token)
	}
	out := make([]store.BotWithTokens, 0, len(botRows))
	for _, row := range botRows {
		tokens := tokensByBot[row.ID]
		if tokens == nil {
			tokens = []store.BotToken{}
		}
		out = append(out, store.BotWithTokens{
			Bot: store.User{
				ID:          row.ID,
				Kind:        row.Kind,
				OwnerUserID: row.OwnerUserID,
				DisplayName: row.DisplayName,
				Handle:      row.Handle,
				AvatarURL:   row.AvatarUrl,
				CreatedAt:   row.CreatedAt,
			},
			Tokens: tokens,
		})
	}
	return out, nil
}

func (s *Store) CreateBotToken(ctx context.Context, input store.CreateBotTokenInput) (store.BotToken, error) {
	botUserID := strings.TrimSpace(input.BotUserID)
	if botUserID == "" {
		return store.BotToken{}, errors.New("bot_user_id is required")
	}
	tokenName := strings.TrimSpace(input.Name)
	if tokenName == "" {
		tokenName = "default"
	}
	scopes, err := normalizeBotScopes(input.Scopes)
	if err != nil {
		return store.BotToken{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.BotToken{}, err
	}
	defer tx.Rollback()
	createdBy := strings.TrimSpace(input.CreatedBy)
	if createdBy == "" {
		return store.BotToken{}, errors.New("created_by is required")
	}
	setupNonce, err := normalizeSetupNonce(input.SetupNonce)
	if err != nil {
		return store.BotToken{}, err
	}
	if setupNonce != "" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, "clickclack.bot-token-setup."+createdBy, setupNonce); err != nil {
			return store.BotToken{}, err
		}
	}
	bot, err := scanUser(tx.QueryRowContext(ctx, `SELECT id, kind, owner_user_id, display_name, handle, avatar_url, created_at FROM users WHERE id = $1`, botUserID))
	if err != nil {
		return store.BotToken{}, err
	}
	if bot.Kind != "bot" {
		return store.BotToken{}, errors.New("bot_user_id must refer to a bot")
	}
	workspaceID, err := botWorkspaceForTokenTx(ctx, tx, bot.ID, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return store.BotToken{}, err
	}
	if err := requireBotTokenManagerTx(ctx, tx, workspaceID, bot, createdBy); err != nil {
		return store.BotToken{}, err
	}
	if setupNonce != "" {
		replayBot, replayToken, replayErr := getSetupBotTokenTx(ctx, tx, createdBy, setupNonce)
		if replayErr == nil {
			if replayToken.WorkspaceID != workspaceID ||
				replayToken.BotUserID != bot.ID ||
				replayToken.Name != tokenName ||
				!botSetupScopesMatch(input.Scopes, replayToken.Scopes, scopes) ||
				replayToken.RevokedAt != nil ||
				replayBot.ID != bot.ID {
				return store.BotToken{}, store.ErrSetupNonceConflict
			}
			replayToken, err = rotateSetupBotTokenTx(ctx, tx, replayToken)
			if err != nil {
				return store.BotToken{}, err
			}
			return replayToken, tx.Commit()
		}
		if !errors.Is(replayErr, sql.ErrNoRows) {
			return store.BotToken{}, replayErr
		}
	}
	token := newID("ccb")
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return store.BotToken{}, err
	}
	botToken := store.BotToken{
		ID:          newID("btok"),
		Token:       token,
		BotUserID:   bot.ID,
		WorkspaceID: workspaceID,
		OwnerUserID: bot.OwnerUserID,
		Name:        tokenName,
		Scopes:      scopes,
		CreatedBy:   createdBy,
		CreatedAt:   now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO bot_tokens (id, token_hash, bot_user_id, workspace_id, owner_user_id, name, scopes_json, created_by, setup_nonce, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		botToken.ID,
		hashBotToken(token),
		botToken.BotUserID,
		botToken.WorkspaceID,
		sqlOptionalText(botToken.OwnerUserID),
		botToken.Name,
		string(scopesJSON),
		sqlOptionalText(botToken.CreatedBy),
		setupNonce,
		botToken.CreatedAt,
	)
	if err != nil {
		return store.BotToken{}, err
	}
	return botToken, tx.Commit()
}

func (s *Store) ListBotTokens(ctx context.Context, botUserID, requesterID string) ([]store.BotToken, error) {
	workspaceID, err := s.botWorkspace(ctx, botUserID)
	if err != nil {
		return nil, err
	}
	return s.ListBotTokensForWorkspace(ctx, workspaceID, botUserID, requesterID)
}

func (s *Store) ListBotTokensForWorkspace(ctx context.Context, workspaceID, botUserID, requesterID string) ([]store.BotToken, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	botUserID = strings.TrimSpace(botUserID)
	if workspaceID == "" {
		return nil, errors.New("workspace_id is required")
	}
	if botUserID == "" {
		return nil, errors.New("bot_user_id is required")
	}
	if _, err := s.getWorkspaceBot(ctx, workspaceID, botUserID); err != nil {
		return nil, err
	}
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	return s.listBotTokensForBotWorkspace(ctx, botUserID, workspaceID)
}

func (s *Store) RevokeBotToken(ctx context.Context, tokenID, requesterID string) (store.BotToken, error) {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return store.BotToken{}, errors.New("token_id is required")
	}
	token, err := s.getBotTokenByID(ctx, tokenID)
	if err != nil {
		return store.BotToken{}, err
	}
	bot, err := s.getWorkspaceBot(ctx, token.WorkspaceID, token.BotUserID)
	if err != nil {
		return store.BotToken{}, err
	}
	if err := s.requireBotTokenManager(ctx, token.WorkspaceID, bot, requesterID); err != nil {
		return store.BotToken{}, err
	}
	revokedAt := now()
	if _, err := s.db.ExecContext(ctx, `UPDATE bot_tokens SET revoked_at = COALESCE(revoked_at, $1) WHERE id = $2`, revokedAt, tokenID); err != nil {
		return store.BotToken{}, err
	}
	return s.getBotTokenByID(ctx, tokenID)
}

func (s *Store) botWorkspace(ctx context.Context, botUserID string) (string, error) {
	return botWorkspaceForToken(ctx, s.db, botUserID)
}

func botWorkspaceForToken(ctx context.Context, db queryer, botUserID string) (string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT wm.workspace_id
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.user_id = $1 AND u.kind = 'bot'
		ORDER BY wm.created_at
		LIMIT 2`, botUserID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var workspaceIDs []string
	for rows.Next() {
		var workspaceID string
		if err := rows.Scan(&workspaceID); err != nil {
			return "", err
		}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(workspaceIDs) == 0 {
		return "", sql.ErrNoRows
	}
	if len(workspaceIDs) > 1 {
		return "", errors.New("workspace_id is required for bots installed in multiple workspaces")
	}
	return workspaceIDs[0], nil
}

func botWorkspaceForTokenTx(ctx context.Context, tx *sql.Tx, botUserID, workspaceID string) (string, error) {
	if workspaceID == "" {
		return botWorkspaceForToken(ctx, tx, botUserID)
	}
	var matched string
	err := tx.QueryRowContext(ctx, `
		SELECT wm.workspace_id
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		WHERE wm.user_id = $1 AND wm.workspace_id = $2 AND u.kind = 'bot'`, botUserID, workspaceID).Scan(&matched)
	return matched, err
}

func (s *Store) listBotTokensForBotWorkspace(ctx context.Context, botUserID, workspaceID string) ([]store.BotToken, error) {
	rows, err := s.db.QueryContext(ctx, botTokenSelect()+`
		WHERE bot_user_id = $1 AND workspace_id = $2
		ORDER BY created_at DESC`, botUserID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBotTokens(rows)
}

func (s *Store) getWorkspaceBot(ctx context.Context, workspaceID, botUserID string) (store.User, error) {
	bot, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.kind, u.owner_user_id, u.display_name, u.handle, u.avatar_url, u.created_at
		FROM users u
		JOIN workspace_members wm ON wm.user_id = u.id
		WHERE wm.workspace_id = $1 AND u.id = $2 AND u.kind = 'bot'`, workspaceID, botUserID))
	if err != nil {
		return store.User{}, err
	}
	return bot, nil
}

func (s *Store) requireBotTokenManager(ctx context.Context, workspaceID string, bot store.User, requesterID string) error {
	requesterID = strings.TrimSpace(requesterID)
	if requesterID == "" {
		return errors.New("requester_id is required")
	}
	if bot.OwnerUserID != "" {
		if requesterID != bot.OwnerUserID {
			return store.ErrBotOwnerRequired
		}
		if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrBotOwnerMembershipRequired
			}
			return err
		}
		return nil
	}
	return s.requireWorkspaceManager(ctx, workspaceID, requesterID)
}

func requireBotTokenManagerTx(ctx context.Context, tx *sql.Tx, workspaceID string, bot store.User, requesterID string) error {
	requesterID = strings.TrimSpace(requesterID)
	if requesterID == "" {
		return errors.New("requester_id is required")
	}
	if bot.OwnerUserID != "" {
		if requesterID != bot.OwnerUserID {
			return store.ErrBotOwnerRequired
		}
		if err := requireMembershipTx(ctx, tx, workspaceID, requesterID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrBotOwnerMembershipRequired
			}
			return err
		}
		return nil
	}
	return requireWorkspaceManagerTx(ctx, tx, workspaceID, requesterID)
}

func (s *Store) RemoveBotFromWorkspace(ctx context.Context, workspaceID, botUserID, requesterID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	botUserID = strings.TrimSpace(botUserID)
	requesterID = strings.TrimSpace(requesterID)
	if workspaceID == "" {
		return errors.New("workspace_id is required")
	}
	if botUserID == "" {
		return errors.New("bot_user_id is required")
	}
	if requesterID == "" {
		return errors.New("requester_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, requesterID); err != nil {
		return err
	}
	qtx := s.q.WithTx(tx)
	if _, err := qtx.LockBotWorkspaceMembership(ctx, storedb.LockBotWorkspaceMembershipParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	}); err != nil {
		return err
	}
	if err := qtx.DeleteBotCommandsForBot(ctx, storedb.DeleteBotCommandsForBotParams{
		WorkspaceID: workspaceID,
		BotUserID:   botUserID,
	}); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspaceID, botUserID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	revokedAt := now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE bot_tokens
		SET revoked_at = COALESCE(revoked_at, $1)
		WHERE bot_user_id = $2 AND workspace_id = $3`, revokedAt, botUserID, workspaceID); err != nil {
		return err
	}
	return tx.Commit()
}

type botDeletionCounts struct {
	slashCommands      int
	eventSubscriptions int
	botTokens          int
}

func (s *Store) DeleteBot(ctx context.Context, botUserID, requesterID string) (store.DeletedBot, error) {
	botUserID = strings.TrimSpace(botUserID)
	requesterID = strings.TrimSpace(requesterID)
	if botUserID == "" {
		return store.DeletedBot{}, errors.New("bot_user_id is required")
	}
	if requesterID == "" {
		return store.DeletedBot{}, errors.New("requester_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.DeletedBot{}, err
	}
	defer tx.Rollback()
	deleted, _, err := s.deleteBotTx(ctx, tx, botUserID, requesterID)
	if err != nil {
		return store.DeletedBot{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.DeletedBot{}, err
	}
	return deleted, nil
}

func (s *Store) deleteBotTx(ctx context.Context, tx *sql.Tx, botUserID, requesterID string) (store.DeletedBot, botDeletionCounts, error) {
	qtx := s.q.WithTx(tx)
	row, err := qtx.GetActiveBotForDeletion(ctx, botUserID)
	if err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	bot := storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt)
	if bot.OwnerUserID != "" {
		if requesterID != bot.OwnerUserID {
			return store.DeletedBot{}, botDeletionCounts{}, store.ErrBotOwnerRequired
		}
	} else {
		workspaceIDs, err := qtx.ListBotGovernedWorkspaces(ctx, bot.ID)
		if err != nil {
			return store.DeletedBot{}, botDeletionCounts{}, err
		}
		if len(workspaceIDs) == 0 {
			workspaceIDs, err = qtx.ListBotHistoricalWorkspaces(ctx, bot.ID)
			if err != nil {
				return store.DeletedBot{}, botDeletionCounts{}, err
			}
		}
		if len(workspaceIDs) == 0 {
			return store.DeletedBot{}, botDeletionCounts{}, store.ErrNotWorkspaceManager
		}
		for _, workspaceID := range workspaceIDs {
			if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, requesterID); err != nil {
				return store.DeletedBot{}, botDeletionCounts{}, err
			}
		}
	}
	deletedAt := now()
	counts := botDeletionCounts{}
	tokenCount, err := qtx.RevokeAllBotTokens(ctx, storedb.RevokeAllBotTokensParams{
		RevokedAt: sqlText(deletedAt),
		BotUserID: bot.ID,
	})
	if err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	counts.botTokens = int(tokenCount)
	commandCount, err := qtx.RevokeAllBotSlashCommands(ctx, storedb.RevokeAllBotSlashCommandsParams{
		RevokedAt: sqlText(deletedAt),
		BotUserID: bot.ID,
	})
	if err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	counts.slashCommands = int(commandCount)
	subscriptionCount, err := qtx.RevokeAllBotEventSubscriptions(ctx, storedb.RevokeAllBotEventSubscriptionsParams{
		RevokedAt: sqlText(deletedAt),
		BotUserID: bot.ID,
	})
	if err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	counts.eventSubscriptions = int(subscriptionCount)
	if _, err := qtx.RevokeAllBotConnectedAccounts(ctx, storedb.RevokeAllBotConnectedAccountsParams{
		RevokedAt: sqlText(deletedAt),
		BotUserID: bot.ID,
	}); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	if _, err := qtx.RevokeAllBotAppInstallations(ctx, storedb.RevokeAllBotAppInstallationsParams{
		RevokedAt: sqlText(deletedAt),
		BotUserID: bot.ID,
	}); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	if err := qtx.DeleteAllBotCommands(ctx, bot.ID); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	if _, err := qtx.DeleteAllBotWorkspaceMemberships(ctx, bot.ID); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	if err := qtx.InsertBotTombstone(ctx, storedb.InsertBotTombstoneParams{
		BotUserID:    bot.ID,
		FormerHandle: bot.Handle,
		DeletedAt:    deletedAt,
		DeletedBy:    sqlOptionalText(requesterID),
	}); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	}
	if rows, err := qtx.RetireBotUser(ctx, bot.ID); err != nil {
		return store.DeletedBot{}, botDeletionCounts{}, err
	} else if rows != 1 {
		return store.DeletedBot{}, botDeletionCounts{}, errors.New("bot changed during deletion")
	}
	return store.DeletedBot{
		ID:           bot.ID,
		DisplayName:  bot.DisplayName,
		FormerHandle: bot.Handle,
		DeletedAt:    deletedAt,
	}, counts, nil
}

func (s *Store) ListBotsOwnedBy(ctx context.Context, ownerUserID string) ([]store.OwnedBotEntry, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, errors.New("owner_user_id is required")
	}
	rows, err := s.q.ListBotsOwnedBy(ctx, sqlOptionalText(ownerUserID))
	if err != nil {
		return nil, err
	}
	out := make([]store.OwnedBotEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, store.OwnedBotEntry{
			Bot: storeUserFromDB(row.ID, row.Kind, row.OwnerUserID, row.DisplayName, row.Handle, row.AvatarUrl, row.CreatedAt),
			Workspace: store.OwnedBotWorkspace{
				ID:      row.WorkspaceID,
				RouteID: row.WorkspaceRouteID,
				Name:    row.WorkspaceName,
			},
			ActiveTokenCount: int(row.ActiveTokenCount),
		})
	}
	return out, nil
}

func (s *Store) getBotTokenByID(ctx context.Context, tokenID string) (store.BotToken, error) {
	return scanBotToken(s.db.QueryRowContext(ctx, botTokenSelect()+` WHERE id = $1`, tokenID))
}

func botTokenSelect() string {
	return `SELECT id, bot_user_id, workspace_id, owner_user_id, name, scopes_json, created_by, created_at, last_used_at, revoked_at FROM bot_tokens`
}

func scanBotTokens(rows *sql.Rows) ([]store.BotToken, error) {
	out := []store.BotToken{}
	for rows.Next() {
		token, err := scanBotToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func scanBotToken(row scanner) (store.BotToken, error) {
	var token store.BotToken
	var ownerUserID, createdBy, lastUsedAt, revokedAt sql.NullString
	var scopesJSON string
	err := row.Scan(
		&token.ID,
		&token.BotUserID,
		&token.WorkspaceID,
		&ownerUserID,
		&token.Name,
		&scopesJSON,
		&createdBy,
		&token.CreatedAt,
		&lastUsedAt,
		&revokedAt,
	)
	if err != nil {
		return store.BotToken{}, err
	}
	if ownerUserID.Valid {
		token.OwnerUserID = ownerUserID.String
	}
	if createdBy.Valid {
		token.CreatedBy = createdBy.String
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.String
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.String
	}
	if err := json.Unmarshal([]byte(scopesJSON), &token.Scopes); err != nil {
		return store.BotToken{}, err
	}
	return token, nil
}

func normalizeBotScopes(values []string) ([]string, error) {
	seen := map[string]bool{}
	var scopes []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			scope := strings.TrimSpace(part)
			if scope == "" {
				continue
			}
			if bundle, ok := botScopeBundles[scope]; ok {
				for _, bundled := range bundle {
					if !seen[bundled] {
						seen[bundled] = true
						scopes = append(scopes, bundled)
					}
				}
				continue
			}
			if !slices.Contains(botAllowedScopes, scope) {
				return nil, errors.New("unknown bot scope: " + scope)
			}
			if !seen[scope] {
				seen[scope] = true
				scopes = append(scopes, scope)
			}
		}
	}
	if len(scopes) == 0 {
		return normalizeBotScopes([]string{"bot:write"})
	}
	slices.Sort(scopes)
	return scopes, nil
}

func normalizeSetupNonce(value string) (string, error) {
	nonce, err := normalizeClientNonce(value)
	if err != nil {
		return "", err
	}
	if nonce != "" && len(nonce) < 16 {
		return "", errors.New("setup_nonce must be at least 16 characters")
	}
	return nonce, nil
}

func botSetupScopesMatch(values, stored, normalized []string) bool {
	if slices.Equal(stored, normalized) {
		return true
	}
	usesWriteBundle := false
	hasScope := false
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			scope := strings.TrimSpace(part)
			if scope == "" {
				continue
			}
			hasScope = true
			if scope == "bot:write" || scope == "bot:admin" {
				usesWriteBundle = true
			}
		}
	}
	if !hasScope {
		usesWriteBundle = true
	}
	if !usesWriteBundle {
		return false
	}
	legacy := slices.DeleteFunc(slices.Clone(normalized), func(scope string) bool {
		return scope == store.BotCommandsWriteScope
	})
	return slices.Equal(stored, legacy)
}

func getSetupBotTokenTx(ctx context.Context, tx *sql.Tx, createdBy, setupNonce string) (store.User, store.BotToken, error) {
	token, err := scanBotToken(tx.QueryRowContext(
		ctx,
		botTokenSelect()+` WHERE created_by = $1 AND setup_nonce = $2 FOR UPDATE`,
		createdBy,
		setupNonce,
	))
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	bot, err := scanUser(tx.QueryRowContext(
		ctx,
		`SELECT id, kind, owner_user_id, display_name, handle, avatar_url, created_at FROM users WHERE id = $1`,
		token.BotUserID,
	))
	return bot, token, err
}

func rotateSetupBotTokenTx(ctx context.Context, tx *sql.Tx, token store.BotToken) (store.BotToken, error) {
	rawToken := newID("ccb")
	result, err := tx.ExecContext(
		ctx,
		`UPDATE bot_tokens SET token_hash = $1, last_used_at = NULL WHERE id = $2 AND revoked_at IS NULL`,
		hashBotToken(rawToken),
		token.ID,
	)
	if err != nil {
		return store.BotToken{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.BotToken{}, err
	}
	if affected != 1 {
		return store.BotToken{}, store.ErrSetupNonceConflict
	}
	token.Token = rawToken
	token.LastUsedAt = nil
	return token, nil
}

func hashBotToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
