package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/cc/transcript?limit=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", owner.ID)
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
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"hidden"}]}}`,
		`{"type":"progress","message":{"role":"assistant","content":"hidden"}}`,
		`not json`,
	}, "\n")
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	messages, err := readCCTranscriptMessages(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if messages == nil || len(messages) != 0 {
		t.Fatalf("expected an empty non-nil message list, got %#v", messages)
	}
}
