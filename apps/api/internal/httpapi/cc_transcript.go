package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ccTruthStatusPathEnv = "CLICKCLACK_CC_TRUTH_STATUS_PATH"

const maxCCTranscriptLineBytes = 8 * 1024 * 1024

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
		writeError(w, http.StatusServiceUnavailable, errors.New("cc transcript bridge status is unavailable"))
		return
	}

	cwd := strings.TrimSpace(r.URL.Query().Get("cwd"))
	selected := selectCCSession(sessions, cwd)
	if selected == nil {
		writeJSON(w, http.StatusOK, ccTranscriptResponse{
			Status:   "No live Claude Code session is attached yet.",
			Sessions: sanitizeCCSessions(sessions),
			Messages: []ccTranscriptMessage{},
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
		writeError(w, http.StatusServiceUnavailable, errors.New("cc transcript is unavailable"))
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
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read transcript %s: %w", path, err)
	}
	defer file.Close()
	if limit <= 0 {
		limit = 20
	}

	messages := make([]ccTranscriptMessage, 0, limit)
	reader := bufio.NewReader(file)
	for {
		line, oversized, err := readCCTranscriptLine(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read transcript %s: %w", path, err)
		}
		if oversized {
			continue
		}
		line = bytes.TrimSpace(line)
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
	return messages, nil
}

func readCCTranscriptLine(reader *bufio.Reader) ([]byte, bool, error) {
	line := make([]byte, 0, 4096)
	oversized := false
	for {
		fragment, isPrefix, err := reader.ReadLine()
		if err != nil {
			return nil, false, err
		}
		if !oversized {
			if len(line)+len(fragment) > maxCCTranscriptLineBytes {
				line = nil
				oversized = true
			} else {
				line = append(line, fragment...)
			}
		}
		if !isPrefix {
			return line, oversized, nil
		}
	}
}

func isCCMetaRow(row map[string]any) bool {
	if row == nil {
		return true
	}
	for _, field := range []string{"isMeta", "isCompactSummary", "isSidechain"} {
		if hidden, ok := row[field].(bool); ok && hidden {
			return true
		}
	}
	typeName, _ := row["type"].(string)
	return typeName != "user" && typeName != "assistant"
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
		content = ccContentText(msg["content"])
	}
	if content == "" {
		content = ccContentText(row["content"])
	}
	content = strings.TrimSpace(content)
	return role, content, timestamp
}

func ccContentText(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			switch block := item.(type) {
			case string:
				if text := strings.TrimSpace(block); text != "" {
					parts = append(parts, text)
				}
			case map[string]any:
				kind, _ := block["type"].(string)
				if kind != "" && kind != "text" {
					continue
				}
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		kind, _ := typed["type"].(string)
		if kind != "" && kind != "text" {
			return ""
		}
		text, _ := typed["text"].(string)
		return text
	default:
		return ""
	}
}
