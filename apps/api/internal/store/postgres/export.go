package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
)

func (s *Store) Backup(ctx context.Context, outPath string) error {
	return errors.New("Postgres backups must be taken with pg_dump or provider snapshots")
}

func (s *Store) ExportJSON(ctx context.Context, writer io.Writer) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	out := map[string]any{}
	tables := []string{
		"users", "user_notification_settings", "identities", "workspaces", "workspace_members", "channels",
		"messages", "thread_state", "reactions", "events", "event_recipients", "uploads",
		"channel_reads", "direct_reads",
		"message_attachments", "direct_conversations", "direct_conversation_members",
		"invites", "auth_magic_links", "sessions", "bot_tokens",
	}
	for _, table := range tables {
		rows, err := tx.QueryContext(ctx, `SELECT * FROM `+table)
		if err != nil {
			return err
		}
		values, err := rowsToMaps(table, rows)
		if err != nil {
			return err
		}
		out[table] = values
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func rowsToMaps(table string, rows *sql.Rows) ([]map[string]any, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		scan := make([]any, len(cols))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			if shouldRedactExportColumn(table, col) {
				row[col] = "[redacted]"
				continue
			}
			switch value := values[i].(type) {
			case []byte:
				row[col] = string(value)
			default:
				row[col] = value
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func shouldRedactExportColumn(table, column string) bool {
	switch table {
	case "user_notification_settings":
		return column == "pushover_user_key"
	case "invites":
		return column == "token"
	case "auth_magic_links", "sessions":
		return column == "token" || column == "token_hash"
	case "bot_tokens":
		return column == "token_hash" || column == "setup_nonce"
	case "uploads":
		return column == "storage_path"
	default:
		return false
	}
}
