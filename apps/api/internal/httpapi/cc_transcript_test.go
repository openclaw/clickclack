package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestCCTranscriptRequiresLocalHumanAndConfiguredBridge(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	bot, botToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspaces[0].ID,
		OwnerUserID: owner.ID,
		DisplayName: "Reader Bot",
		Scopes:      []string{"bot:read"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspaces[0].ID, bot.ID, store.WorkspaceRoleBot); err != nil {
		t.Fatal(err)
	}

	handler := New(st, realtime.NewHub(), Options{}).Handler()
	remoteReq := httptest.NewRequest(http.MethodGet, "/api/cc/transcript", nil)
	remoteReq.RemoteAddr = "203.0.113.10:45678"
	remoteReq.Host = "127.0.0.1:8080"
	remoteRec := httptest.NewRecorder()
	handler.ServeHTTP(remoteRec, remoteReq)
	if remoteRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated remote transcript request to be unauthorized, got %d", remoteRec.Code)
	}
	session, err := st.CreateSession(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	remoteSessionReq := httptest.NewRequest(http.MethodGet, "/api/cc/transcript", nil)
	remoteSessionReq.RemoteAddr = "203.0.113.10:45678"
	remoteSessionReq.Host = "127.0.0.1:8080"
	remoteSessionReq.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	remoteSessionRec := httptest.NewRecorder()
	handler.ServeHTTP(remoteSessionRec, remoteSessionReq)
	if remoteSessionRec.Code != http.StatusForbidden {
		t.Fatalf("expected authenticated remote transcript request to be forbidden, got %d", remoteSessionRec.Code)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	botReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript", nil)
	if err != nil {
		t.Fatal(err)
	}
	botReq.Header.Set("Authorization", "Bearer "+botToken.Token)
	botResp, err := http.DefaultClient.Do(botReq)
	if err != nil {
		t.Fatal(err)
	}
	botResp.Body.Close()
	if botResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected bot transcript request to be forbidden, got %s", botResp.Status)
	}

	localReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript", nil)
	if err != nil {
		t.Fatal(err)
	}
	localReq.Header.Set("X-ClickClack-User", owner.ID)
	localResp, err := http.DefaultClient.Do(localReq)
	if err != nil {
		t.Fatal(err)
	}
	localResp.Body.Close()
	if localResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected unconfigured transcript bridge to fail closed, got %s", localResp.Status)
	}
}

func TestCCTranscriptConfiguredBridgeReturnsSanitizedMessages(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}

	transcriptPath := filepath.Join(dataDir, "session.jsonl")
	transcript := strings.Join([]string{
		`{"type":"system","isMeta":true,"content":"ignore me"}`,
		`{"type":"user","timestamp":"2026-07-04T10:00:00Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-07-04T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"hi back"},{"type":"tool_use","name":"ignored"}]}}`,
	}, "\n")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	statusScript := filepath.Join(dataDir, "cc_truth_status.py")
	statusScriptBody := `import json
print(json.dumps([{
  "lane": "now",
  "truth_status": "active",
  "api_status": "busy",
  "session_id": "sess_1",
  "cwd": "/tmp/clickclack",
  "transcript": ` + strconv.Quote(transcriptPath) + `,
  "why": "test"
}]))
`
	if err := os.WriteFile(statusScript, []byte(statusScriptBody), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ccTruthStatusPathEnv, statusScript)
	t.Setenv(ccTruthAccessTokenEnv, "test-transcript-token")

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	missingTokenReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript", nil)
	if err != nil {
		t.Fatal(err)
	}
	missingTokenReq.Header.Set("X-ClickClack-User", owner.ID)
	missingTokenResp, err := http.DefaultClient.Do(missingTokenReq)
	if err != nil {
		t.Fatal(err)
	}
	missingTokenResp.Body.Close()
	if missingTokenResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected missing transcript access token to return 403, got %s", missingTokenResp.Status)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
	req.Header.Set(ccTruthAccessTokenHeader, "test-transcript-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected configured transcript bridge to return 200, got %s", resp.Status)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["session"].(map[string]any)["transcript"]; ok {
		t.Fatalf("transcript path leaked in session payload: %#v", payload["session"])
	}
	for _, item := range payload["sessions"].([]any) {
		if _, ok := item.(map[string]any)["transcript"]; ok {
			t.Fatalf("transcript path leaked in sessions payload: %#v", payload["sessions"])
		}
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one limited transcript message, got %#v", payload["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "assistant" || message["content"] != "hi back" {
		t.Fatalf("unexpected transcript message: %#v", messages[0])
	}

	missingPath := filepath.Join(dataDir, "private-missing.jsonl")
	missingScriptBody := strings.Replace(statusScriptBody, strconv.Quote(transcriptPath), strconv.Quote(missingPath), 1)
	if err := os.WriteFile(statusScript, []byte(missingScriptBody), 0o600); err != nil {
		t.Fatal(err)
	}
	missingReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript", nil)
	if err != nil {
		t.Fatal(err)
	}
	missingReq.Header.Set("X-ClickClack-User", owner.ID)
	missingReq.Header.Set(ccTruthAccessTokenHeader, "test-transcript-token")
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatal(err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected a missing transcript to return 503, got %s", missingResp.Status)
	}
	var failure map[string]any
	if err := json.NewDecoder(missingResp.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(failure["error"].(string), missingPath) {
		t.Fatalf("transcript path leaked in failure payload: %#v", failure)
	}
}

func TestReadCCTranscriptMessagesReturnsHonestEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	rows := strings.Join([]string{
		`{"type":"system","isMeta":true,"content":"hidden"}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"hidden"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":"hidden"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"hidden"}]}}`,
		`{"type":"progress","message":{"role":"assistant","content":"hidden"}}`,
		`not json`,
	}, "\n")
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := readCCTranscriptMessages(context.Background(), path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if messages == nil || len(messages) != 0 {
		t.Fatalf("expected an empty non-nil message list, got %#v", messages)
	}
}

func TestReadCCTranscriptMessagesSkipsOversizedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.jsonl")
	oversized := `{"type":"progress","content":"` + strings.Repeat("x", maxCCTranscriptLineBytes) + `"}`
	oversizedMessage := `{"type":"assistant","message":{"role":"assistant","content":"` + strings.Repeat("x", maxCCTranscriptMessageBytes+1) + `"}}`
	rows := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"before"}}`,
		oversized,
		oversizedMessage,
		`{"type":"assistant","message":{"role":"assistant","content":"after"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := readCCTranscriptMessages(context.Background(), path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Content != "before" || messages[1].Content != "after" {
		t.Fatalf("expected surrounding messages to survive oversized row, got %#v", messages)
	}
}

func TestLoadCCTruthSessionsBoundsHelper(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "slow.py")
		if err := os.WriteFile(script, []byte("import time\ntime.sleep(10)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		if _, err := loadCCTruthSessions(ctx, script); err == nil {
			t.Fatal("expected timed-out helper to fail")
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("helper ignored context timeout: %s", elapsed)
		}
	})

	t.Run("output", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "noisy.py")
		body := fmt.Sprintf("import sys\nsys.stdout.write('x' * %d)\n", maxCCTruthStatusOutputBytes+1)
		if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadCCTruthSessions(context.Background(), script); err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("expected bounded helper output error, got %v", err)
		}
	})
}

func TestReadCCTranscriptMessagesBoundsResponseBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	rows := make([]string, 0, 12)
	for i := range 12 {
		content := fmt.Sprintf("%02d:", i) + strings.Repeat("x", 200*1024)
		rows = append(rows, fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":%q}}`, content))
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := readCCTranscriptMessages(context.Background(), path, 200)
	if err != nil {
		t.Fatal(err)
	}
	totalBytes := 0
	for _, message := range messages {
		totalBytes += len(message.Content) + len(message.Timestamp)
	}
	if totalBytes > maxCCTranscriptTotalBytes {
		t.Fatalf("response exceeded byte budget: %d", totalBytes)
	}
	if len(messages) != 10 {
		t.Fatalf("expected ten messages within byte budget, got %d", len(messages))
	}
	if !strings.HasPrefix(messages[0].Content, "02:") || !strings.HasPrefix(messages[9].Content, "11:") {
		t.Fatalf("expected the newest ten messages, got %d from %.3q to %.3q", len(messages), messages[0].Content, messages[len(messages)-1].Content)
	}
}

func TestReadCCTranscriptMessagesReadsBoundedRegularFileTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	rows := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"outside window"}}`,
		`{"type":"progress","content":"` + strings.Repeat("x", maxCCTranscriptInputBytes) + `"}`,
		`{"type":"assistant","message":{"role":"assistant","content":"inside window"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := readCCTranscriptMessages(context.Background(), path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "inside window" {
		t.Fatalf("expected only the bounded tail message, got %#v", messages)
	}
	if _, err := readCCTranscriptMessages(context.Background(), t.TempDir(), 20); err == nil {
		t.Fatal("expected non-regular transcript path to fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readCCTranscriptMessages(ctx, path, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled transcript read, got %v", err)
	}
}
