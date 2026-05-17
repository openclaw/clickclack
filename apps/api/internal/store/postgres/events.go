package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store/postgres/storedb"
)

// PruneEvents deletes old durable events for a workspace while preserving the
// newest keepLatest events. At least one retention bound is required so callers
// cannot accidentally wipe the whole event log with default zero values.
func (s *Store) PruneEvents(ctx context.Context, workspaceID string, keepLatest int, before string) (int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	before = strings.TrimSpace(before)
	if workspaceID == "" {
		return 0, errors.New("workspace is required")
	}
	if keepLatest < 0 {
		return 0, errors.New("keep_latest must be non-negative")
	}
	if keepLatest == 0 && before == "" {
		return 0, errors.New("keep_latest or before is required")
	}
	if before != "" {
		parsed, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			return 0, errors.New("before must be RFC3339")
		}
		before = parsed.UTC().Format(time.RFC3339Nano)
	}
	return s.q.PruneEvents(ctx, storedb.PruneEventsParams{
		WorkspaceIDArg: workspaceID,
		Before:         before,
		KeepLatest:     int32(keepLatest),
	})
}
