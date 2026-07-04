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
		`{"type":"assistant","timestamp":"2026-07-04T10:00:01Z","message":{"role":"assistant","content":"hi back"}}`,
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
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one limited transcript message, got %#v", payload["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "assistant" || message["content"] != "hi back" {
		t.Fatalf("unexpected transcript message: %#v", messages[0])
	}
}
