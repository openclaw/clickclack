package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

// botSetupCodeTTL bounds how long a pending setup code can be claimed.
const botSetupCodeTTL = 10 * time.Minute

// setupCodeAlphabet is crockford-style base32 without the ambiguous
// I, L, O, and U. 12 characters give 60 bits of entropy, which is
// required because the claim endpoint is unauthenticated.
const setupCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const setupCodeLength = 12

func newBotSetupCode() (string, error) {
	var raw [setupCodeLength]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	var out strings.Builder
	for i, b := range raw {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		// 256 is a multiple of 32, so masking has no modulo bias.
		out.WriteByte(setupCodeAlphabet[b&31])
	}
	return out.String(), nil
}

func normalizeBotSetupCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	if len(code) != setupCodeLength {
		return "", store.ErrSetupCodeInvalid
	}
	for i := 0; i < len(code); i++ {
		if !strings.ContainsRune(setupCodeAlphabet, rune(code[i])) {
			return "", store.ErrSetupCodeInvalid
		}
	}
	return code, nil
}

func hashBotSetupCode(normalizedCode string) string {
	sum := sha256.Sum256([]byte("bot-setup-code:" + normalizedCode))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateBotSetupCode(ctx context.Context, input store.CreateBotSetupCodeInput) (store.BotSetupCode, error) {
	botUserID := strings.TrimSpace(input.BotUserID)
	if botUserID == "" {
		return store.BotSetupCode{}, errors.New("bot_user_id is required")
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return store.BotSetupCode{}, errors.New("workspace_id is required")
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	if createdBy == "" {
		return store.BotSetupCode{}, errors.New("created_by is required")
	}
	tokenName := strings.TrimSpace(input.Name)
	if tokenName == "" {
		tokenName = "default"
	}
	scopes, err := normalizeBotScopes(input.Scopes)
	if err != nil {
		return store.BotSetupCode{}, err
	}
	code, err := newBotSetupCode()
	if err != nil {
		return store.BotSetupCode{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.BotSetupCode{}, err
	}
	defer tx.Rollback()
	bot, err := lockActiveBotTx(ctx, tx, botUserID)
	if err != nil {
		return store.BotSetupCode{}, err
	}
	if _, err := botWorkspaceForTokenTx(ctx, tx, bot.ID, workspaceID); err != nil {
		return store.BotSetupCode{}, err
	}
	if err := requireBotTokenManagerTx(ctx, tx, workspaceID, bot, createdBy); err != nil {
		return store.BotSetupCode{}, err
	}
	qtx := s.q.WithTx(tx)
	createdAt := now()
	if _, err := qtx.DeleteExpiredBotSetupCodes(ctx, createdAt); err != nil {
		return store.BotSetupCode{}, err
	}
	// Re-minting replaces any pending code for the same token grant.
	if _, err := qtx.DeleteUnclaimedBotSetupCodesForTokenName(ctx, storedb.DeleteUnclaimedBotSetupCodesForTokenNameParams{
		WorkspaceID: workspaceID,
		BotUserID:   bot.ID,
		TokenName:   tokenName,
	}); err != nil {
		return store.BotSetupCode{}, err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return store.BotSetupCode{}, err
	}
	setup := store.BotSetupCode{
		ID:          newID("bsc"),
		BotUserID:   bot.ID,
		WorkspaceID: workspaceID,
		TokenName:   tokenName,
		Scopes:      scopes,
		CreatedBy:   createdBy,
		CreatedAt:   createdAt,
		ExpiresAt:   time.Now().UTC().Add(botSetupCodeTTL).Format(time.RFC3339Nano),
	}
	if err := qtx.InsertBotSetupCode(ctx, storedb.InsertBotSetupCodeParams{
		ID:          setup.ID,
		CodeHash:    hashBotSetupCode(strings.ReplaceAll(code, "-", "")),
		WorkspaceID: setup.WorkspaceID,
		BotUserID:   setup.BotUserID,
		TokenName:   setup.TokenName,
		ScopesJson:  string(scopesJSON),
		CreatedBy:   sqlOptionalText(setup.CreatedBy),
		CreatedAt:   setup.CreatedAt,
		ExpiresAt:   setup.ExpiresAt,
	}); err != nil {
		return store.BotSetupCode{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.BotSetupCode{}, err
	}
	setup.Code = code
	return setup, nil
}

func (s *Store) ClaimBotSetupCode(ctx context.Context, code string) (store.BotSetupCodeClaim, error) {
	normalized, err := normalizeBotSetupCode(code)
	if err != nil {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	codeHash := hashBotSetupCode(normalized)
	candidate, err := qtx.GetBotSetupCodeByHash(ctx, codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	// Take the lifecycle lock before locking the code row. Bot removal and
	// deletion use the same order, preventing a code-row/lifecycle deadlock.
	bot, err := lockActiveBotTx(ctx, tx, candidate.BotUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	row, err := qtx.LockBotSetupCodeByHash(ctx, codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	if row.BotUserID != bot.ID {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	if row.ClaimedAt.Valid {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, row.ExpiresAt)
	if err != nil || !time.Now().UTC().Before(expiresAt) {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	workspaceID, err := botWorkspaceForTokenTx(ctx, tx, bot.ID, row.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	var scopes []string
	if err := json.Unmarshal([]byte(row.ScopesJson), &scopes); err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	token := newID("ccb")
	botToken := store.BotToken{
		ID:          newID("btok"),
		Token:       token,
		BotUserID:   bot.ID,
		WorkspaceID: workspaceID,
		OwnerUserID: bot.OwnerUserID,
		Name:        row.TokenName,
		Scopes:      scopes,
		CreatedBy:   stringFromNull(row.CreatedBy),
		CreatedAt:   now(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bot_tokens (id, token_hash, bot_user_id, workspace_id, owner_user_id, name, scopes_json, created_by, setup_nonce, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		botToken.ID,
		hashBotToken(token),
		botToken.BotUserID,
		botToken.WorkspaceID,
		sqlOptionalText(botToken.OwnerUserID),
		botToken.Name,
		row.ScopesJson,
		sqlOptionalText(botToken.CreatedBy),
		"",
		botToken.CreatedAt,
	); err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	claimed, err := qtx.MarkBotSetupCodeClaimed(ctx, storedb.MarkBotSetupCodeClaimedParams{
		ClaimedAt:      sqlText(botToken.CreatedAt),
		ClaimedTokenID: sqlText(botToken.ID),
		ID:             row.ID,
	})
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	if claimed != 1 {
		return store.BotSetupCodeClaim{}, store.ErrSetupCodeInvalid
	}
	wsRow, err := qtx.GetWorkspaceForSetupClaim(ctx, workspaceID)
	if err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	defaultChannel, err := qtx.GetDefaultChannelForSetupClaim(ctx, workspaceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.BotSetupCodeClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.BotSetupCodeClaim{}, err
	}
	claim := store.BotSetupCodeClaim{
		BotToken: botToken,
		Bot:      bot,
		Workspace: store.Workspace{
			ID:        wsRow.ID,
			RouteID:   stringFromNull(wsRow.RouteID),
			Name:      wsRow.Name,
			Slug:      wsRow.Slug,
			IconURL:   wsRow.IconUrl,
			CreatedAt: wsRow.CreatedAt,
		},
	}
	if defaultChannel != "" {
		claim.Defaults.DefaultTo = "channel:" + defaultChannel
	}
	return claim, nil
}
