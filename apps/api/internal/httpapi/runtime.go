package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// getChannelRuntime serves the per-channel agent runtime snapshot (model,
// thinking, context used/limit) that the composer model picker and context
// meter render. Session-authenticated read; bot tokens are gated to their
// workspace. The data originates from the agent-bridge reading the gateway
// directly, with no dependency on ClawCanvas.
func (s *Server) getChannelRuntime(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:read"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	channelID := chi.URLParam(r, "channel_id")
	if !s.requireBotChannelWorkspace(w, r, act, channelID) {
		return
	}
	// Enforce the caller's access to the channel (membership / guest rules)
	// the same way message reads do.
	if _, err := s.store.GetChannel(r.Context(), channelID, act.user.ID); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	rec, err := s.store.GetChannelRuntime(r.Context(), channelID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime": rec})
}

// putChannelRuntime is the bridge stamp: it writes the gateway-sourced runtime
// snapshot. It requires a bot token carrying agent_activity:write (the same
// dedicated, non-inherited scope the agent-activity message path requires), so
// a plain human session or a bot:write token cannot forge runtime facts.
func (s *Server) putChannelRuntime(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID == "" {
		writeError(w, http.StatusForbidden, errors.New("agent runtime updates require a bot token"))
		return
	}
	if err := act.requireScope(store.AgentActivityWriteScope); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	channelID := chi.URLParam(r, "channel_id")
	if !s.requireBotChannelWorkspace(w, r, act, channelID) {
		return
	}
	var body struct {
		DefaultModel     string          `json:"default_model"`
		DefaultThinking  string          `json:"default_thinking"`
		Model            string          `json:"model"`
		Thinking         string          `json:"thinking"`
		ContextUsed      int64           `json:"context_used"`
		ContextLimit     int64           `json:"context_limit"`
		CacheHitPct      *float64        `json:"cache_hit_pct"`
		ContextBreakdown json.RawMessage `json:"context_breakdown"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rec, err := s.store.UpsertChannelRuntime(r.Context(), channelID, store.ChannelRuntimeSnapshot{
		DefaultModel:     body.DefaultModel,
		DefaultThinking:  body.DefaultThinking,
		Model:            body.Model,
		Thinking:         body.Thinking,
		ContextUsed:      body.ContextUsed,
		ContextLimit:     body.ContextLimit,
		CacheHitPct:      body.CacheHitPct,
		ContextBreakdown: body.ContextBreakdown,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime": rec})
}

// patchChannelRuntime records the picker's desired model/thinking override for
// the next turn. Session-authenticated; the bridge consumes the override and
// applies it to the gateway session. Writing here never clobbers the
// bridge-owned snapshot.
func (s *Server) patchChannelRuntime(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if err := act.requireScope("messages:write"); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	channelID := chi.URLParam(r, "channel_id")
	if !s.requireBotChannelWorkspace(w, r, act, channelID) {
		return
	}
	if _, err := s.store.GetChannel(r.Context(), channelID, act.user.ID); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var body struct {
		Model    string `json:"model"`
		Thinking string `json:"thinking"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rec, err := s.store.SetChannelRuntimeOverride(r.Context(), channelID, store.ChannelRuntimeOverride{
		Model:    body.Model,
		Thinking: body.Thinking,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime": rec})
}
