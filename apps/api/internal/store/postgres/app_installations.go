package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) ListAppInstallations(ctx context.Context, workspaceID, requesterID string) ([]store.AppInstallation, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, appInstallationSelect()+`
		WHERE workspace_id = $1 AND revoked_at IS NULL
		ORDER BY app_slug`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppInstallations(rows)
}

func (s *Store) CreateAppInstallation(ctx context.Context, input store.CreateAppInstallationInput) (store.AppInstallation, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	appSlug := slug(input.AppSlug)
	if workspaceID == "" {
		return store.AppInstallation{}, errors.New("workspace_id is required")
	}
	if appSlug == "" {
		return store.AppInstallation{}, errors.New("app_slug is required")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = appSlug
	}
	if len(displayName) > 80 {
		return store.AppInstallation{}, errors.New("display_name is too long")
	}
	botUserID := strings.TrimSpace(input.BotUserID)
	if botUserID == "" {
		return store.AppInstallation{}, errors.New("bot_user_id is required")
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return store.AppInstallation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.AppInstallation{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.AppInstallation{}, err
	}
	var botKind string
	if err := tx.QueryRowContext(ctx, `
		SELECT u.kind
		FROM users u
		JOIN workspace_members wm ON wm.user_id = u.id
		WHERE u.id = $1 AND wm.workspace_id = $2`, botUserID, workspaceID).Scan(&botKind); err != nil {
		return store.AppInstallation{}, err
	}
	if botKind != "bot" {
		return store.AppInstallation{}, errors.New("bot_user_id must refer to a bot in the workspace")
	}
	installation := store.AppInstallation{
		ID:          newID("app"),
		WorkspaceID: workspaceID,
		AppSlug:     appSlug,
		DisplayName: displayName,
		BotUserID:   botUserID,
		Config:      config,
		CreatedBy:   createdBy,
		CreatedAt:   now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO app_installations (id, workspace_id, app_slug, display_name, bot_user_id, config_json, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		installation.ID,
		installation.WorkspaceID,
		installation.AppSlug,
		installation.DisplayName,
		installation.BotUserID,
		string(configJSON),
		sqlOptionalText(installation.CreatedBy),
		installation.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_app_installations_active_slug") {
			return store.AppInstallation{}, errors.New("app is already installed")
		}
		return store.AppInstallation{}, err
	}
	return installation, tx.Commit()
}

func (s *Store) RevokeAppInstallation(ctx context.Context, installationID, requesterID string, options store.RevokeAppInstallationOptions) (store.RevokeAppInstallationResult, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return store.RevokeAppInstallationResult{}, errors.New("installation_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.RevokeAppInstallationResult{}, err
	}
	defer tx.Rollback()
	installation, err := scanAppInstallation(tx.QueryRowContext(ctx, appInstallationSelect()+` WHERE id = $1 FOR UPDATE`, installationID))
	if err != nil {
		return store.RevokeAppInstallationResult{}, err
	}
	if err := requireWorkspaceManagerTx(ctx, tx, installation.WorkspaceID, requesterID); err != nil {
		return store.RevokeAppInstallationResult{}, err
	}
	result := store.RevokeAppInstallationResult{Installation: installation}
	if installation.RevokedAt != nil {
		if err := tx.Commit(); err != nil {
			return store.RevokeAppInstallationResult{}, err
		}
		return result, nil
	}
	revokedAt := now()
	count, err := revokeRows(ctx, tx, `UPDATE app_installations SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`, revokedAt, installationID)
	if err != nil {
		return store.RevokeAppInstallationResult{}, err
	}
	if count != 1 {
		return store.RevokeAppInstallationResult{}, errors.New("app installation changed during revoke")
	}
	if options.RevokeSlashCommands {
		count, err = revokeRows(ctx, tx, `UPDATE slash_commands SET revoked_at = $1 WHERE app_installation_id = $2 AND revoked_at IS NULL`, revokedAt, installationID)
		if err != nil {
			return store.RevokeAppInstallationResult{}, err
		}
		result.Revoked.SlashCommands = count
	}
	if options.RevokeEventSubscriptions {
		count, err = revokeRows(ctx, tx, `UPDATE event_subscriptions SET revoked_at = $1 WHERE app_installation_id = $2 AND revoked_at IS NULL`, revokedAt, installationID)
		if err != nil {
			return store.RevokeAppInstallationResult{}, err
		}
		result.Revoked.EventSubscriptions = count
	}
	if options.RevokeBotTokens {
		count, err = revokeRows(ctx, tx, `UPDATE bot_tokens SET revoked_at = $1 WHERE workspace_id = $2 AND bot_user_id = $3 AND revoked_at IS NULL`, revokedAt, installation.WorkspaceID, installation.BotUserID)
		if err != nil {
			return store.RevokeAppInstallationResult{}, err
		}
		result.Revoked.BotTokens = count
	}
	result.Installation.RevokedAt = &revokedAt
	if err := tx.Commit(); err != nil {
		return store.RevokeAppInstallationResult{}, err
	}
	return result, nil
}

func revokeRows(ctx context.Context, tx *sql.Tx, query string, args ...any) (int, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *Store) getAppInstallation(ctx context.Context, installationID string) (store.AppInstallation, error) {
	return scanAppInstallation(s.db.QueryRowContext(ctx, appInstallationSelect()+` WHERE id = $1`, installationID))
}

func appInstallationSelect() string {
	return `SELECT id, workspace_id, app_slug, display_name, bot_user_id, config_json, created_by, created_at, revoked_at FROM app_installations`
}

func scanAppInstallations(rows *sql.Rows) ([]store.AppInstallation, error) {
	out := []store.AppInstallation{}
	for rows.Next() {
		installation, err := scanAppInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, installation)
	}
	return out, rows.Err()
}

func scanAppInstallation(row scanner) (store.AppInstallation, error) {
	var installation store.AppInstallation
	var configJSON string
	var createdBy, revokedAt sql.NullString
	if err := row.Scan(
		&installation.ID,
		&installation.WorkspaceID,
		&installation.AppSlug,
		&installation.DisplayName,
		&installation.BotUserID,
		&configJSON,
		&createdBy,
		&installation.CreatedAt,
		&revokedAt,
	); err != nil {
		return store.AppInstallation{}, err
	}
	if createdBy.Valid {
		installation.CreatedBy = createdBy.String
	}
	if revokedAt.Valid {
		installation.RevokedAt = &revokedAt.String
	}
	if err := json.Unmarshal([]byte(configJSON), &installation.Config); err != nil {
		return store.AppInstallation{}, err
	}
	if installation.Config == nil {
		installation.Config = map[string]any{}
	}
	return installation, nil
}
