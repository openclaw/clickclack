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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.User{}, store.BotToken{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if input.OwnerUserID != "" {
		ownerRow, err := qtx.GetUser(ctx, input.OwnerUserID)
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
	bot := store.User{
		ID:          newID("usr"),
		Kind:        "bot",
		OwnerUserID: strings.TrimSpace(input.OwnerUserID),
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
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
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

func hashBotToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
