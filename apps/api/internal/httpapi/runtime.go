package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

// validOverrideThinking is the closed set a picker override may set. The empty
// string clears the thinking override. These mirror the gateway's accepted
// thinking levels; anything else is rejected at the API boundary so a channel
// writer cannot persist a value the bridge would blindly apply to a session.
var validOverrideThinking = map[string]bool{
	"":         true,
	"off":      true,
	"minimal":  true,
	"low":      true,
	"medium":   true,
	"high":     true,
	"xhigh":    true,
	"adaptive": true,
}

// maxOverrideModelLen bounds the picker override model string. Real model ids
// are well under this; the cap exists to stop a member writing a multi-KB blob
// into the channel's next-turn override row.
const maxOverrideModelLen = 256

// validateRuntimeOverride enforces the member-writable picker override contract.
func validateRuntimeOverride(model, thinking string) error {
	if len(model) > maxOverrideModelLen {
		return fmt.Errorf("model must be at most %d characters", maxOverrideModelLen)
	}
	if strings.ContainsAny(model, "\n\r\x00") {
		return errors.New("model must not contain control characters")
	}
	if !validOverrideThinking[thinking] {
		return fmt.Errorf("thinking %q is not an accepted value", thinking)
	}
	return nil
}

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
	if err := validateRuntimeOverride(body.Model, body.Thinking); err != nil {
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
