package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// GetChannelRuntime returns the per-channel runtime snapshot. A channel with no
// snapshot yet returns an empty record (ChannelID set) rather than an error.
func (s *Store) GetChannelRuntime(ctx context.Context, channelID string) (store.ChannelRuntime, error) {
	const q = `SELECT channel_id, default_model, default_thinking, model, thinking,
		override_model, override_thinking, context_used, context_limit,
		cache_hit_pct, context_breakdown, updated_at
		FROM channel_runtime WHERE channel_id = $1`
	return scanChannelRuntime(s.db.QueryRowContext(ctx, q, channelID), channelID)
}

// UpsertChannelRuntime writes the bridge-owned fields, leaving any pending
// picker override untouched.
func (s *Store) UpsertChannelRuntime(ctx context.Context, channelID string, snap store.ChannelRuntimeSnapshot) (store.ChannelRuntime, error) {
	var breakdown any
	if len(snap.ContextBreakdown) > 0 {
		breakdown = string(snap.ContextBreakdown)
	}
	var cache any
	if snap.CacheHitPct != nil {
		cache = *snap.CacheHitPct
	}
	const q = `INSERT INTO channel_runtime
		(channel_id, default_model, default_thinking, model, thinking,
		 context_used, context_limit, cache_hit_pct, context_breakdown, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT(channel_id) DO UPDATE SET
			default_model = EXCLUDED.default_model,
			default_thinking = EXCLUDED.default_thinking,
			model = EXCLUDED.model,
			thinking = EXCLUDED.thinking,
			context_used = EXCLUDED.context_used,
			context_limit = EXCLUDED.context_limit,
			cache_hit_pct = EXCLUDED.cache_hit_pct,
			context_breakdown = EXCLUDED.context_breakdown,
			updated_at = EXCLUDED.updated_at`
	if _, err := s.db.ExecContext(ctx, q, channelID, snap.DefaultModel, snap.DefaultThinking,
		snap.Model, snap.Thinking, snap.ContextUsed, snap.ContextLimit, cache, breakdown, now()); err != nil {
		return store.ChannelRuntime{}, err
	}
	return s.GetChannelRuntime(ctx, channelID)
}

// SetChannelRuntimeOverride writes only the picker-owned override fields.
func (s *Store) SetChannelRuntimeOverride(ctx context.Context, channelID string, override store.ChannelRuntimeOverride) (store.ChannelRuntime, error) {
	const q = `INSERT INTO channel_runtime (channel_id, override_model, override_thinking, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(channel_id) DO UPDATE SET
			override_model = EXCLUDED.override_model,
			override_thinking = EXCLUDED.override_thinking,
			updated_at = EXCLUDED.updated_at`
	if _, err := s.db.ExecContext(ctx, q, channelID, override.Model, override.Thinking, now()); err != nil {
		return store.ChannelRuntime{}, err
	}
	return s.GetChannelRuntime(ctx, channelID)
}

func scanChannelRuntime(row *sql.Row, channelID string) (store.ChannelRuntime, error) {
	var (
		rec       = store.ChannelRuntime{ChannelID: channelID}
		cache     sql.NullFloat64
		breakdown sql.NullString
	)
	err := row.Scan(&rec.ChannelID, &rec.DefaultModel, &rec.DefaultThinking, &rec.Model, &rec.Thinking,
		&rec.OverrideModel, &rec.OverrideThinking, &rec.ContextUsed, &rec.ContextLimit,
		&cache, &breakdown, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return store.ChannelRuntime{ChannelID: channelID}, nil
	}
	if err != nil {
		return store.ChannelRuntime{}, err
	}
	if cache.Valid {
		rec.CacheHitPct = &cache.Float64
	}
	if breakdown.Valid && breakdown.String != "" {
		rec.ContextBreakdown = json.RawMessage(breakdown.String)
	}
	return rec, nil
}
