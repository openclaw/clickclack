package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func (s *Store) CreateAuditLogEntry(ctx context.Context, input store.CreateAuditLogEntryInput) (store.AuditLogEntry, error) {
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return store.AuditLogEntry{}, err
	}
	entry := store.AuditLogEntry{
		ID:          newID("aud"),
		WorkspaceID: strings.TrimSpace(input.WorkspaceID),
		ActorUserID: strings.TrimSpace(input.ActorUserID),
		Action:      strings.TrimSpace(input.Action),
		TargetType:  strings.TrimSpace(input.TargetType),
		TargetID:    strings.TrimSpace(input.TargetID),
		Metadata:    metadata,
		CreatedAt:   now(),
	}
	if entry.WorkspaceID == "" || entry.ActorUserID == "" || entry.Action == "" || entry.TargetType == "" || entry.TargetID == "" {
		return store.AuditLogEntry{}, errors.New("audit log entry is incomplete")
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_log_entries (id, workspace_id, actor_user_id, action, target_type, target_id, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.WorkspaceID,
		entry.ActorUserID,
		entry.Action,
		entry.TargetType,
		entry.TargetID,
		string(metadataJSON),
		entry.CreatedAt,
	)
	return entry, err
}

func (s *Store) ListAuditLogEntries(ctx context.Context, workspaceID, requesterID string, limit int) ([]store.AuditLogEntry, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, auditLogEntrySelect()+`
		WHERE workspace_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditLogEntries(rows)
}

func (s *Store) ListConnectedAccounts(ctx context.Context, workspaceID, requesterID string) ([]store.ConnectedAccount, error) {
	if err := s.requireMembership(ctx, workspaceID, requesterID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, connectedAccountSelect()+`
		WHERE workspace_id = ? AND revoked_at IS NULL
		ORDER BY provider, display_name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnectedAccounts(rows)
}

func (s *Store) CreateConnectedAccount(ctx context.Context, input store.CreateConnectedAccountInput) (store.ConnectedAccount, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	userID := strings.TrimSpace(input.UserID)
	createdBy := strings.TrimSpace(input.CreatedBy)
	provider := slug(input.Provider)
	providerAccountID := strings.TrimSpace(input.ProviderAccountID)
	if workspaceID == "" || userID == "" || provider == "" || providerAccountID == "" {
		return store.ConnectedAccount{}, errors.New("connected account is incomplete")
	}
	scopes, err := normalizeStringList(input.Scopes)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	defer tx.Rollback()
	if err := requireWorkspaceManagerTx(ctx, tx, workspaceID, createdBy); err != nil {
		return store.ConnectedAccount{}, err
	}
	if err := requireMembershipTx(ctx, tx, workspaceID, userID); err != nil {
		return store.ConnectedAccount{}, err
	}
	account := store.ConnectedAccount{
		ID:                newID("acct"),
		WorkspaceID:       workspaceID,
		UserID:            userID,
		Provider:          provider,
		ProviderAccountID: providerAccountID,
		DisplayName:       strings.TrimSpace(input.DisplayName),
		Scopes:            scopes,
		Metadata:          metadata,
		CreatedAt:         now(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO connected_accounts (id, workspace_id, user_id, provider, provider_account_id, display_name, scopes_json, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID,
		account.WorkspaceID,
		account.UserID,
		account.Provider,
		account.ProviderAccountID,
		account.DisplayName,
		string(scopesJSON),
		string(metadataJSON),
		account.CreatedAt,
	)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	return account, tx.Commit()
}

func (s *Store) RevokeConnectedAccount(ctx context.Context, accountID, requesterID string) (store.ConnectedAccount, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return store.ConnectedAccount{}, errors.New("account_id is required")
	}
	account, err := s.getConnectedAccount(ctx, accountID)
	if err != nil {
		return store.ConnectedAccount{}, err
	}
	if account.UserID != requesterID {
		if err := s.requireWorkspaceManager(ctx, account.WorkspaceID, requesterID); err != nil {
			return store.ConnectedAccount{}, err
		}
	}
	revokedAt := now()
	if _, err := s.db.ExecContext(ctx, `UPDATE connected_accounts SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, revokedAt, accountID); err != nil {
		return store.ConnectedAccount{}, err
	}
	return s.getConnectedAccount(ctx, accountID)
}

func (s *Store) getConnectedAccount(ctx context.Context, accountID string) (store.ConnectedAccount, error) {
	return scanConnectedAccount(s.db.QueryRowContext(ctx, connectedAccountSelect()+` WHERE id = ?`, accountID))
}

func normalizeStringList(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		if len(value) > 120 {
			return nil, errors.New("list value is too long")
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
}

func auditLogEntrySelect() string {
	return `SELECT id, workspace_id, actor_user_id, action, target_type, target_id, metadata_json, created_at FROM audit_log_entries`
}

func scanAuditLogEntries(rows *sql.Rows) ([]store.AuditLogEntry, error) {
	out := []store.AuditLogEntry{}
	for rows.Next() {
		var entry store.AuditLogEntry
		var metadataJSON string
		if err := rows.Scan(&entry.ID, &entry.WorkspaceID, &entry.ActorUserID, &entry.Action, &entry.TargetType, &entry.TargetID, &metadataJSON, &entry.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metadataJSON), &entry.Metadata); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func connectedAccountSelect() string {
	return `SELECT id, workspace_id, user_id, provider, provider_account_id, display_name, scopes_json, metadata_json, created_at, revoked_at FROM connected_accounts`
}

func scanConnectedAccounts(rows *sql.Rows) ([]store.ConnectedAccount, error) {
	out := []store.ConnectedAccount{}
	for rows.Next() {
		account, err := scanConnectedAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, account)
	}
	return out, rows.Err()
}

func scanConnectedAccount(row scanner) (store.ConnectedAccount, error) {
	var account store.ConnectedAccount
	var scopesJSON, metadataJSON string
	var revokedAt sql.NullString
	if err := row.Scan(&account.ID, &account.WorkspaceID, &account.UserID, &account.Provider, &account.ProviderAccountID, &account.DisplayName, &scopesJSON, &metadataJSON, &account.CreatedAt, &revokedAt); err != nil {
		return store.ConnectedAccount{}, err
	}
	if err := json.Unmarshal([]byte(scopesJSON), &account.Scopes); err != nil {
		return store.ConnectedAccount{}, err
	}
	if err := json.Unmarshal([]byte(metadataJSON), &account.Metadata); err != nil {
		return store.ConnectedAccount{}, err
	}
	if revokedAt.Valid {
		account.RevokedAt = &revokedAt.String
	}
	return account, nil
}
