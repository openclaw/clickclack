package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const (
	routeIDInsertAttempts = 5
	routeIDMigrationName  = "0011_public_route_ids.sql"
	routeIDBackfillMarker = "0011_public_route_ids.backfill"
)

func isRouteIDConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "route_id")
}

func (s *Store) backfillRouteIDsOnce(ctx context.Context) error {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = $1`, routeIDMigrationName).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = $1`, routeIDBackfillMarker).Scan(&name)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := s.backfillRouteIDs(ctx); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO schema_migrations (name, applied_at)
		VALUES ($1, $2)
		ON CONFLICT(name) DO NOTHING`, routeIDBackfillMarker, now())
	return err
}

func (s *Store) backfillRouteIDs(ctx context.Context) error {
	if err := s.backfillTableRouteIDs(ctx, "workspaces", "T", `route_id IS NULL`); err != nil {
		return err
	}
	if err := s.backfillTableRouteIDs(ctx, "channels", "C", `route_id IS NULL`); err != nil {
		return err
	}
	if err := s.backfillTableRouteIDs(ctx, "direct_conversations", "D", `route_id IS NULL`); err != nil {
		return err
	}
	return s.backfillTableRouteIDs(ctx, "messages", "M", `
		route_id IS NULL
		AND parent_message_id IS NULL
		AND EXISTS (
		  SELECT 1
		  FROM thread_state ts
		  WHERE ts.root_message_id = messages.id
		    AND ts.reply_count > 0
		)`)
}

func (s *Store) backfillTableRouteIDs(ctx context.Context, table, prefix, where string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+where+` ORDER BY id`)
	if err != nil {
		if strings.Contains(err.Error(), "no such column: route_id") || strings.Contains(err.Error(), "column \"route_id\" does not exist") {
			return nil
		}
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.assignRouteID(ctx, table, id, prefix[0]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) assignRouteID(ctx context.Context, table, id string, prefix byte) error {
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		routeID, err := newRouteID(prefix)
		if err != nil {
			return err
		}
		res, err := s.db.ExecContext(ctx, `UPDATE `+table+` SET route_id = $1 WHERE id = $2 AND route_id IS NULL`, routeID, id)
		if err == nil {
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return nil
			}
			return nil
		}
		if !isRouteIDConflict(err) {
			return err
		}
	}
	return sql.ErrNoRows
}

func assignRouteIDTx(ctx context.Context, tx *sql.Tx, table, id string, prefix byte) error {
	for attempt := 0; attempt < routeIDInsertAttempts; attempt++ {
		routeID, err := newRouteID(prefix)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE `+table+` SET route_id = $1 WHERE id = $2 AND route_id IS NULL`, routeID, id)
		if err == nil {
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return nil
			}
			return nil
		}
		if !isRouteIDConflict(err) {
			return err
		}
	}
	return errors.New("could not assign route_id after collision retries")
}

func ensureThreadRouteIDTx(ctx context.Context, tx *sql.Tx, root store.Message) (store.Message, error) {
	if root.ParentMessageID != nil {
		return store.Message{}, errors.New("thread root must be a root message")
	}
	if root.RouteID == "" {
		if err := assignRouteIDTx(ctx, tx, "messages", root.ID, 'M'); err != nil {
			return store.Message{}, err
		}
	}
	return getMessageTx(ctx, tx, root.ID)
}

func (s *Store) EnsureThreadRouteID(ctx context.Context, userID, rootMessageID string) (store.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Message{}, err
	}
	defer tx.Rollback()
	root, err := getMessageTx(ctx, tx, rootMessageID)
	if err != nil {
		return store.Message{}, err
	}
	if err := requireMessageAccessTx(ctx, tx, root, userID); err != nil {
		return store.Message{}, err
	}
	root, err = ensureThreadRouteIDTx(ctx, tx, root)
	if err != nil {
		return store.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Message{}, err
	}
	return root, nil
}
