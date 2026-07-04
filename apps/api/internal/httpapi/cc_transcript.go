package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ccTruthStatusPathEnv = "CLICKCLACK_CC_TRUTH_STATUS_PATH"

// ccTruthSession mirrors the bridge status rows produced by cc_truth_status.py.
type ccTruthSession struct {
	Lane                 string   `json:"lane"`
	TruthStatus          string   `json:"truth_status"`
	ApiStatus            *string  `json:"api_status"`
	Pid                  *int     `json:"pid"`
	ProcessState         *string  `json:"process_state"`
	CPUTicksDelta        *int     `json:"cpu_ticks_delta"`
	Duplicates           *int     `json:"duplicates"`
	SessionID            string   `json:"session_id"`
	Cwd                  string   `json:"cwd"`
	LastOutputAgeSeconds *float64 `json:"last_output_age_seconds"`
	LastOutputRole       *string  `json:"last_output_role"`
	LastOutputSnippet    *string  `json:"last_output_snippet"`
	Transcript           string   `json:"transcript"`
	Why                  string   `json:"why"`
}

type ccTranscriptSession struct {
	Lane                 string   `json:"lane"`
	TruthStatus          string   `json:"truth_status"`
	ApiStatus            *string  `json:"api_status,omitempty"`
	Pid                  *int     `json:"pid,omitempty"`
	ProcessState         *string  `json:"process_state,omitempty"`
	CPUTicksDelta        *int     `json:"cpu_ticks_delta,omitempty"`
	Duplicates           *int     `json:"duplicates,omitempty"`
	SessionID            string   `json:"session_id"`
	Cwd                  string   `json:"cwd"`
	LastOutputAgeSeconds *float64 `json:"last_output_age_seconds,omitempty"`
	LastOutputRole       *string  `json:"last_output_role,omitempty"`
	LastOutputSnippet    *string  `json:"last_output_snippet,omitempty"`
	Why                  string   `json:"why,omitempty"`
}

type ccTranscriptMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp,omitempty"`
}

type ccTranscriptResponse struct {
	Status   string                `json:"status"`
	Session  *ccTranscriptSession  `json:"session,omitempty"`
	Sessions []ccTranscriptSession `json:"sessions"`
	Messages []ccTranscriptMessage `json:"messages"`
}

func (s *Server) ccTranscript(w http.ResponseWriter, r *http.Request) {
	act, err := s.currentActor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if act.botTokenID != "" {
		writeError(w, http.StatusForbidden, errors.New("cc transcript is only available to human local sessions"))
		return
	}
	if !isLocalDevRequest(r) {
		writeError(w, http.StatusForbidden, errors.New("cc transcript is only available from loopback clients"))
		return
	}

	script := strings.TrimSpace(os.Getenv(ccTruthStatusPathEnv))
	if script == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("cc transcript bridge is not configured"))
		return
	}
	sessions, err := loadCCTruthSessions(r.Context(), script)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}

	cwd := strings.TrimSpace(r.URL.Query().Get("cwd"))
	selected := selectCCSession(sessions, cwd)
	if selected == nil {
		writeJSON(w, http.StatusOK, ccTranscriptResponse{
			Status:   "No live Claude Code session is attached yet.",
			Sessions: sanitizeCCSessions(sessions),
			Messages: nil,
		})
		return
	}

	limit := queryInt(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	messages, err := readCCTranscriptMessages(selected.Transcript, limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}

	status := fmt.Sprintf("%s · %s · %s", selected.Lane, selected.TruthStatus, selected.Cwd)
	if selected.Why != "" {
		status = status + " — " + selected.Why
	}

	writeJSON(w, http.StatusOK, ccTranscriptResponse{
		Status:   status,
		Session:  sanitizeCCSession(*selected),
		Sessions: sanitizeCCSessions(sessions),
		Messages: messages,
	})
}

func loadCCTruthSessions(ctx context.Context, script string) ([]ccTruthSession, error) {
	cmd := exec.CommandContext(ctx, "python3", script, "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cc truth status failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var sessions []ccTruthSession
	if err := json.Unmarshal(output, &sessions); err != nil {
		return nil, fmt.Errorf("decode cc truth status: %w", err)
	}
	return sessions, nil
}

func sanitizeCCSessions(sessions []ccTruthSession) []ccTranscriptSession {
	out := make([]ccTranscriptSession, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, *sanitizeCCSession(session))
	}
	return out
}

func sanitizeCCSession(session ccTruthSession) *ccTranscriptSession {
	return &ccTranscriptSession{
		Lane:                 session.Lane,
		TruthStatus:          session.TruthStatus,
		ApiStatus:            session.ApiStatus,
		Pid:                  session.Pid,
		ProcessState:         session.ProcessState,
		CPUTicksDelta:        session.CPUTicksDelta,
		Duplicates:           session.Duplicates,
		SessionID:            session.SessionID,
		Cwd:                  session.Cwd,
		LastOutputAgeSeconds: session.LastOutputAgeSeconds,
		LastOutputRole:       session.LastOutputRole,
		LastOutputSnippet:    session.LastOutputSnippet,
		Why:                  session.Why,
	}
}

func selectCCSession(sessions []ccTruthSession, cwd string) *ccTruthSession {
	cleanedCwd := normalizePath(cwd)
	if cleanedCwd != "" {
		for i := range sessions {
			if normalizePath(sessions[i].Cwd) == cleanedCwd {
				return &sessions[i]
			}
		}
	}
	for i := range sessions {
		if sessions[i].TruthStatus == "active" {
			return &sessions[i]
		}
	}
	for i := range sessions {
		if sessions[i].ApiStatus != nil && *sessions[i].ApiStatus == "busy" {
			return &sessions[i]
		}
	}
	for i := range sessions {
		if sessions[i].TruthStatus == "api_busy_unverified" {
			return &sessions[i]
		}
	}
	for i := range sessions {
		if sessions[i].TruthStatus == "idle" {
			return &sessions[i]
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	return &sessions[0]
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func readCCTranscriptMessages(path string, limit int) ([]ccTranscriptMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transcript %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte("\n"))
	messages := make([]ccTranscriptMessage, 0, limit)
	for _, rawLine := range lines {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		if isCCMetaRow(row) {
			continue
		}
		role, content, timestamp := extractCCTranscriptMessage(row)
		if role == "" || content == "" {
			continue
		}
		messages = append(messages, ccTranscriptMessage{
			Role:      role,
			Content:   content,
			Timestamp: timestamp,
		})
		if limit > 0 && len(messages) > limit {
			messages = messages[len(messages)-limit:]
		}
	}
	if len(messages) == 0 {
		return nil, errors.New("no readable CC messages found in transcript")
	}
	return messages, nil
}

func isCCMetaRow(row map[string]any) bool {
	if row == nil {
		return true
	}
	if meta, ok := row["isMeta"].(bool); ok && meta {
		return true
	}
	typeName, _ := row["type"].(string)
	switch typeName {
	case "last-prompt", "mode", "permission-mode", "file-history-snapshot":
		return true
	}
	if typeName == "user" || typeName == "assistant" {
		return false
	}
	if _, ok := row["attachment"]; ok {
		return true
	}
	return false
}

func extractCCTranscriptMessage(row map[string]any) (role string, content string, timestamp string) {
	typeName, _ := row["type"].(string)
	if typeName == "user" || typeName == "assistant" {
		role = typeName
	}
	if ts, ok := row["timestamp"].(string); ok {
		timestamp = ts
	}
	if msg, ok := row["message"].(map[string]any); ok {
		if msgRole, ok := msg["role"].(string); ok && msgRole != "" {
			role = msgRole
		}
		content = anyToString(msg["content"])
	}
	if content == "" {
		content = anyToString(row["content"])
	}
	content = strings.TrimSpace(content)
	return role, content, timestamp
}

func anyToString(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(b)
	}
}
