package sqlite

import (
	"context"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// TestChannelRuntimeSnapshotOverrideSeparation proves the core backbone
// guarantee: a bridge snapshot write and a picker override write own disjoint
// columns, so neither clobbers the other. It also checks that an unknown
// channel reads back as an empty (non-error) record.
func TestChannelRuntimeSnapshotOverrideSeparation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	const channelID = "chn_runtime_test"

	// 1. Unknown channel: empty record, no error.
	rec, err := st.GetChannelRuntime(ctx, channelID)
	if err != nil {
		t.Fatalf("GetChannelRuntime(unknown) error: %v", err)
	}
	if rec.ChannelID != channelID || rec.Model != "" || rec.ContextLimit != 0 {
		t.Fatalf("expected empty record for unknown channel, got %#v", rec)
	}

	// 2. Bridge snapshot write.
	cache := 0.91
	rec, err = st.UpsertChannelRuntime(ctx, channelID, store.ChannelRuntimeSnapshot{
		DefaultModel:     "anthropic/claude-opus-4-8",
		DefaultThinking:  "adaptive",
		Model:            "openai/gpt-5.4",
		Thinking:         "high",
		ContextUsed:      100000,
		ContextLimit:     1000000,
		CacheHitPct:      &cache,
		ContextBreakdown: []byte(`[{"label":"system","tokens":4000}]`),
	})
	if err != nil {
		t.Fatalf("UpsertChannelRuntime error: %v", err)
	}
	if rec.Model != "openai/gpt-5.4" || rec.ContextUsed != 100000 || rec.ContextLimit != 1000000 {
		t.Fatalf("snapshot not persisted: %#v", rec)
	}
	if rec.CacheHitPct == nil || *rec.CacheHitPct != cache {
		t.Fatalf("cache_hit_pct not persisted: %#v", rec.CacheHitPct)
	}
	if string(rec.ContextBreakdown) != `[{"label":"system","tokens":4000}]` {
		t.Fatalf("context_breakdown not persisted: %s", rec.ContextBreakdown)
	}

	// 3. Picker override write must NOT clobber the snapshot.
	rec, err = st.SetChannelRuntimeOverride(ctx, channelID, store.ChannelRuntimeOverride{
		Model:    "openai/gpt-5.4-mini",
		Thinking: "low",
	})
	if err != nil {
		t.Fatalf("SetChannelRuntimeOverride error: %v", err)
	}
	if rec.OverrideModel != "openai/gpt-5.4-mini" || rec.OverrideThinking != "low" {
		t.Fatalf("override not persisted: %#v", rec)
	}
	if rec.Model != "openai/gpt-5.4" || rec.ContextUsed != 100000 {
		t.Fatalf("override clobbered the bridge snapshot: %#v", rec)
	}

	// 4. A fresh bridge snapshot must NOT clobber the pending override.
	rec, err = st.UpsertChannelRuntime(ctx, channelID, store.ChannelRuntimeSnapshot{
		DefaultModel:    "anthropic/claude-opus-4-8",
		DefaultThinking: "adaptive",
		Model:           "anthropic/claude-opus-4-8",
		Thinking:        "adaptive",
		ContextUsed:     120000,
		ContextLimit:    1000000,
	})
	if err != nil {
		t.Fatalf("second UpsertChannelRuntime error: %v", err)
	}
	if rec.ContextUsed != 120000 || rec.Model != "anthropic/claude-opus-4-8" {
		t.Fatalf("second snapshot not applied: %#v", rec)
	}
	if rec.OverrideModel != "openai/gpt-5.4-mini" || rec.OverrideThinking != "low" {
		t.Fatalf("snapshot write clobbered the override: %#v", rec)
	}
}
