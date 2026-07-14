package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) ListSlashCommands(ctx context.Context, workspaceID, requesterID string) ([]store.SlashCommand, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, slashCommandSelect(false)+`
		WHERE workspace_id = ? AND revoked_at IS NULL
		ORDER BY command`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSlashCommands(rows)
}

func (s *Store) CreateSlashCommand(ctx context.Context, input store.CreateSlashCommandInput) (store.SlashCommand, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return store.SlashCommand{}, errors.New("workspace_id is required")
	}
	command := normalizeSlashCommand(input.Command)
	if command == "" {
		return store.SlashCommand{}, errors.New("command is required")
	}
	callbackURL, err := normalizeCallbackURL(input.CallbackURL)
	if err != nil {
		return store.SlashCommand{}, err
	}
	botUserID := strings.TrimSpace(input.BotUserID)
	if botUserID == "" {
		return store.SlashCommand{}, errors.New("bot_user_id is required")
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	description := strings.TrimSpace(input.Description)
	if len(description) > 500 {
		return store.SlashCommand{}, errors.New("description is too long")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.SlashCommand{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.SlashCommand{}, err
	}
	if err := requireWorkspaceBotTx(ctx, tx, workspaceID, botUserID); err != nil {
		return store.SlashCommand{}, err
	}
	appInstallationID := strings.TrimSpace(input.AppInstallationID)
	if appInstallationID != "" {
		var one int
		if err := tx.QueryRowContext(ctx, `
			SELECT 1
			FROM app_installations
			WHERE id = ? AND workspace_id = ? AND revoked_at IS NULL`, appInstallationID, workspaceID).Scan(&one); err != nil {
			return store.SlashCommand{}, err
		}
	}
	cmd := store.SlashCommand{
		ID:                newID("cmd"),
		WorkspaceID:       workspaceID,
		AppInstallationID: appInstallationID,
		Command:           command,
		Description:       description,
		CallbackURL:       callbackURL,
		SigningSecret:     newID("ccs"),
		BotUserID:         botUserID,
		CreatedBy:         createdBy,
		CreatedAt:         now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO slash_commands (id, workspace_id, app_installation_id, command, description, callback_url, signing_secret, bot_user_id, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cmd.ID,
		cmd.WorkspaceID,
		sqlOptionalText(cmd.AppInstallationID),
		cmd.Command,
		cmd.Description,
		cmd.CallbackURL,
		cmd.SigningSecret,
		cmd.BotUserID,
		sqlOptionalText(cmd.CreatedBy),
		cmd.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "idx_slash_commands_active_command") {
			return store.SlashCommand{}, errors.New("slash command is already registered")
		}
		return store.SlashCommand{}, err
	}
	return cmd, tx.Commit()
}

func (s *Store) RevokeSlashCommand(ctx context.Context, commandID, requesterID string) (store.SlashCommand, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return store.SlashCommand{}, errors.New("command_id is required")
	}
	cmd, err := s.getSlashCommand(ctx, commandID, true)
	if err != nil {
		return store.SlashCommand{}, err
	}
	if err := s.requireWorkspaceManager(ctx, cmd.WorkspaceID, requesterID); err != nil {
		return store.SlashCommand{}, err
	}
	revokedAt := now()
	if _, err := s.db.ExecContext(ctx, `UPDATE slash_commands SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, revokedAt, commandID); err != nil {
		return store.SlashCommand{}, err
	}
	return s.getSlashCommand(ctx, commandID, false)
}

func (s *Store) RotateSlashCommandSecret(ctx context.Context, commandID, requesterID string) (store.SlashCommand, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return store.SlashCommand{}, errors.New("command_id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.SlashCommand{}, err
	}
	defer tx.Rollback()
	cmd, err := scanSlashCommand(tx.QueryRowContext(ctx, slashCommandSelect(true)+` WHERE id = ?`, commandID))
	if err != nil {
		return store.SlashCommand{}, err
	}
	if err := requireWorkspaceManagerTx(ctx, tx, cmd.WorkspaceID, requesterID); err != nil {
		return store.SlashCommand{}, err
	}
	if cmd.RevokedAt != nil {
		return store.SlashCommand{}, errors.New("cannot rotate a revoked slash command")
	}
	secret := newID("ccs")
	result, err := tx.ExecContext(ctx, `UPDATE slash_commands SET signing_secret = ? WHERE id = ? AND revoked_at IS NULL`, secret, commandID)
	if err != nil {
		return store.SlashCommand{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.SlashCommand{}, err
	}
	if affected != 1 {
		return store.SlashCommand{}, errors.New("cannot rotate a revoked slash command")
	}
	cmd.SigningSecret = secret
	if err := tx.Commit(); err != nil {
		return store.SlashCommand{}, err
	}
	return cmd, nil
}

func (s *Store) GetSlashCommandForChannel(ctx context.Context, channelID, command, requesterID string) (store.SlashCommand, error) {
	command = normalizeSlashCommand(command)
	return scanSlashCommand(s.db.QueryRowContext(ctx, slashCommandSelect(true)+`
		JOIN channels c ON c.workspace_id = sc.workspace_id
		JOIN workspace_members wm ON wm.workspace_id = sc.workspace_id AND wm.user_id = ?
		WHERE c.id = ? AND sc.command = ? AND sc.revoked_at IS NULL`,
		requesterID,
		channelID,
		command,
	))
}

func (s *Store) CreateSlashCommandInvocation(ctx context.Context, input store.CreateSlashCommandInvocationInput) (store.SlashCommandInvocation, error) {
	invocation := store.SlashCommandInvocation{
		ID:          newID("sci"),
		CommandID:   strings.TrimSpace(input.CommandID),
		WorkspaceID: strings.TrimSpace(input.WorkspaceID),
		ChannelID:   strings.TrimSpace(input.ChannelID),
		UserID:      strings.TrimSpace(input.UserID),
		Text:        strings.TrimSpace(input.Text),
		PayloadJSON: input.PayloadJSON,
		CreatedAt:   now(),
	}
	if invocation.CommandID == "" || invocation.WorkspaceID == "" || invocation.ChannelID == "" || invocation.UserID == "" {
		return store.SlashCommandInvocation{}, errors.New("slash command invocation is incomplete")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO slash_command_invocations (id, command_id, workspace_id, channel_id, user_id, text, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		invocation.ID,
		invocation.CommandID,
		invocation.WorkspaceID,
		invocation.ChannelID,
		invocation.UserID,
		invocation.Text,
		invocation.PayloadJSON,
		invocation.CreatedAt,
	)
	return invocation, err
}

func (s *Store) CompleteSlashCommandInvocation(ctx context.Context, invocationID string, status int, responseBody, invokeError string) (store.SlashCommandInvocation, error) {
	completedAt := now()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE slash_command_invocations
		SET response_status = ?, response_body = ?, error = ?, completed_at = ?
		WHERE id = ?`, status, responseBody, invokeError, completedAt, invocationID); err != nil {
		return store.SlashCommandInvocation{}, err
	}
	return scanSlashCommandInvocation(s.db.QueryRowContext(ctx, slashCommandInvocationSelect()+` WHERE id = ?`, invocationID))
}

func (s *Store) getSlashCommand(ctx context.Context, commandID string, includeSecret bool) (store.SlashCommand, error) {
	return scanSlashCommand(s.db.QueryRowContext(ctx, slashCommandSelect(includeSecret)+` WHERE id = ?`, commandID))
}

func requireWorkspaceBotTx(ctx context.Context, tx *sql.Tx, workspaceID, botUserID string) error {
	var kind string
	if err := tx.QueryRowContext(ctx, `
		SELECT u.kind
		FROM users u
		JOIN workspace_members wm ON wm.user_id = u.id
		WHERE u.id = ? AND wm.workspace_id = ?`, botUserID, workspaceID).Scan(&kind); err != nil {
		return err
	}
	if kind != "bot" {
		return errors.New("bot_user_id must refer to a bot in the workspace")
	}
	return nil
}

func normalizeSlashCommand(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return ""
	}
	if !strings.HasPrefix(command, "/") {
		command = "/" + command
	}
	return command
}

func normalizeCallbackURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("callback_url is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("callback_url must be an http or https URL")
	}
	return value, nil
}

func slashCommandSelect(includeSecret bool) string {
	secret := "''"
	if includeSecret {
		secret = "sc.signing_secret"
	}
	return `SELECT sc.id, sc.workspace_id, sc.app_installation_id, sc.command, sc.description, sc.callback_url, ` + secret + `, sc.bot_user_id, sc.created_by, sc.created_at, sc.revoked_at FROM slash_commands sc`
}

func scanSlashCommands(rows *sql.Rows) ([]store.SlashCommand, error) {
	out := []store.SlashCommand{}
	for rows.Next() {
		cmd, err := scanSlashCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cmd)
	}
	return out, rows.Err()
}

func scanSlashCommand(row scanner) (store.SlashCommand, error) {
	var cmd store.SlashCommand
	var appInstallationID, createdBy, revokedAt sql.NullString
	if err := row.Scan(
		&cmd.ID,
		&cmd.WorkspaceID,
		&appInstallationID,
		&cmd.Command,
		&cmd.Description,
		&cmd.CallbackURL,
		&cmd.SigningSecret,
		&cmd.BotUserID,
		&createdBy,
		&cmd.CreatedAt,
		&revokedAt,
	); err != nil {
		return store.SlashCommand{}, err
	}
	if appInstallationID.Valid {
		cmd.AppInstallationID = appInstallationID.String
	}
	if createdBy.Valid {
		cmd.CreatedBy = createdBy.String
	}
	if revokedAt.Valid {
		cmd.RevokedAt = &revokedAt.String
	}
	return cmd, nil
}

func slashCommandInvocationSelect() string {
	return `SELECT id, command_id, workspace_id, channel_id, user_id, text, payload_json, response_status, response_body, error, created_at, completed_at FROM slash_command_invocations`
}

func scanSlashCommandInvocation(row scanner) (store.SlashCommandInvocation, error) {
	var invocation store.SlashCommandInvocation
	var completedAt sql.NullString
	if err := row.Scan(
		&invocation.ID,
		&invocation.CommandID,
		&invocation.WorkspaceID,
		&invocation.ChannelID,
		&invocation.UserID,
		&invocation.Text,
		&invocation.PayloadJSON,
		&invocation.ResponseStatus,
		&invocation.ResponseBody,
		&invocation.Error,
		&invocation.CreatedAt,
		&completedAt,
	); err != nil {
		return store.SlashCommandInvocation{}, err
	}
	if completedAt.Valid {
		invocation.CompletedAt = &completedAt.String
	}
	return invocation, nil
}
