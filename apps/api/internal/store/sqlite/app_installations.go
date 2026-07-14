package sqlite

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
		WHERE workspace_id = ? AND revoked_at IS NULL
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
		WHERE u.id = ? AND wm.workspace_id = ?`, botUserID, workspaceID).Scan(&botKind); err != nil {
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
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

func (s *Store) RevokeAppInstallation(ctx context.Context, installationID, requesterID string) (store.AppInstallation, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return store.AppInstallation{}, errors.New("installation_id is required")
	}
	installation, err := s.getAppInstallation(ctx, installationID)
	if err != nil {
		return store.AppInstallation{}, err
	}
	if err := s.requireWorkspaceManager(ctx, installation.WorkspaceID, requesterID); err != nil {
		return store.AppInstallation{}, err
	}
	revokedAt := now()
	if _, err := s.db.ExecContext(ctx, `UPDATE app_installations SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, revokedAt, installationID); err != nil {
		return store.AppInstallation{}, err
	}
	return s.getAppInstallation(ctx, installationID)
}

func (s *Store) getAppInstallation(ctx context.Context, installationID string) (store.AppInstallation, error) {
	return scanAppInstallation(s.db.QueryRowContext(ctx, appInstallationSelect()+` WHERE id = ?`, installationID))
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
