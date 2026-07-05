package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ccTruthStatusPathEnv = "CLICKCLACK_CC_TRUTH_STATUS_PATH"

const (
	ccTruthAccessTokenEnv    = "CLICKCLACK_CC_TRANSCRIPT_TOKEN"
	ccTruthAccessTokenHeader = "X-ClickClack-Transcript-Token"
)

const maxCCTranscriptLineBytes = 8 * 1024 * 1024

const (
	ccTruthStatusTimeout        = 5 * time.Second
	maxCCTruthStatusOutputBytes = 1024 * 1024
	maxCCTruthStatusErrorBytes  = 64 * 1024
	maxCCTranscriptMessageBytes = 256 * 1024
	maxCCTranscriptTotalBytes   = 2 * 1024 * 1024
	maxCCTranscriptInputBytes   = 16 * 1024 * 1024
)

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
	accessToken := strings.TrimSpace(os.Getenv(ccTruthAccessTokenEnv))
	if accessToken == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("cc transcript bridge access token is not configured"))
		return
	}
	providedToken := r.Header.Get(ccTruthAccessTokenHeader)
	if subtle.ConstantTimeCompare([]byte(providedToken), []byte(accessToken)) != 1 {
		writeError(w, http.StatusForbidden, errors.New("cc transcript bridge access token is required"))
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
	messages, err := readCCTranscriptMessages(r.Context(), selected.Transcript, limit)
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
	probeCtx, cancel := context.WithTimeout(ctx, ccTruthStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "python3", script, "--json")
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := newCCBoundedOutput(maxCCTruthStatusOutputBytes)
	stderr := newCCBoundedOutput(maxCCTruthStatusErrorBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("cc truth status timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("cc truth status failed: %w", err)
	}
	if stdout.truncated {
		return nil, errors.New("cc truth status output exceeded the size limit")
	}
	var sessions []ccTruthSession
	if err := json.Unmarshal(stdout.Bytes(), &sessions); err != nil {
		return nil, fmt.Errorf("decode cc truth status: %w", err)
	}
	return sessions, nil
}

type ccBoundedOutput struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func newCCBoundedOutput(limit int) *ccBoundedOutput {
	return &ccBoundedOutput{limit: limit}
}

func (w *ccBoundedOutput) Write(p []byte) (int, error) {
	written := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = w.truncated || written > 0
		return written, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(p)
	return written, nil
}

func (w *ccBoundedOutput) Bytes() []byte {
	return w.buffer.Bytes()
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

func readCCTranscriptMessages(ctx context.Context, path string, limit int) ([]ccTranscriptMessage, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat transcript %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("transcript %s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read transcript %s: %w", path, err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat open transcript %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open transcript %s is not a regular file", path)
	}
	if limit <= 0 {
		limit = 20
	}
	start := info.Size() - maxCCTranscriptInputBytes
	if start < 0 {
		start = 0
	}
	partialFirstLine := false
	if start > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], start-1); err != nil {
			return nil, fmt.Errorf("inspect transcript %s: %w", path, err)
		}
		partialFirstLine = previous[0] != '\n'
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek transcript %s: %w", path, err)
	}

	messages := make([]ccTranscriptMessage, 0, limit)
	totalBytes := 0
	reader := bufio.NewReader(io.LimitReader(file, maxCCTranscriptInputBytes))
	if partialFirstLine {
		if _, _, err := readCCTranscriptLine(ctx, reader); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read transcript %s: %w", path, err)
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, oversized, err := readCCTranscriptLine(ctx, reader)
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
		if role == "" || content == "" || len(content) > maxCCTranscriptMessageBytes {
			continue
		}
		message := ccTranscriptMessage{
			Role:      role,
			Content:   content,
			Timestamp: timestamp,
		}
		messages = append(messages, message)
		totalBytes += len(message.Content) + len(message.Timestamp)
		for len(messages) > limit || totalBytes > maxCCTranscriptTotalBytes {
			totalBytes -= len(messages[0].Content) + len(messages[0].Timestamp)
			messages[0] = ccTranscriptMessage{}
			messages = messages[1:]
		}
	}
	return messages, nil
}

func readCCTranscriptLine(ctx context.Context, reader *bufio.Reader) ([]byte, bool, error) {
	line := make([]byte, 0, 4096)
	oversized := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
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
		if msgRole, ok := msg["role"].(string); ok && (msgRole == "user" || msgRole == "assistant") {
			role = msgRole
		}
		content = ccContentText(msg["content"])
	}
	if content == "" {
		content = ccContentText(row["content"])
	}
	content = strings.TrimSpace(content)
	if len(timestamp) > 128 {
		timestamp = ""
	}
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
