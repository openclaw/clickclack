package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
)

func (s *Store) Backup(ctx context.Context, outPath string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, outPath)
	return err
}

func (s *Store) ExportJSON(ctx context.Context, writer io.Writer) error {
	out := map[string]any{}
	tables := []string{
		"users", "user_notification_settings", "identities", "workspaces", "workspace_members", "channels",
		"messages", "thread_state", "reactions", "events", "uploads",
		"message_attachments", "direct_conversations", "direct_conversation_members",
		"invites", "auth_magic_links", "sessions",
	}
	for _, table := range tables {
		rows, err := s.db.QueryContext(ctx, `SELECT * FROM `+table)
		if err != nil {
			return err
		}
		values, err := rowsToMaps(rows)
		if err != nil {
			return err
		}
		out[table] = values
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
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
