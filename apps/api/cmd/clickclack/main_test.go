package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/config"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestDispatchArgsDefaultsNoArgumentInvocationToServe(t *testing.T) {
	cmd, args, clientArgs := dispatchArgs([]string{"clickclack"})
	if cmd != "serve" || len(args) != 0 || len(clientArgs) != 0 {
		t.Fatalf("unexpected dispatch: cmd=%q args=%v clientArgs=%v", cmd, args, clientArgs)
	}
}

func TestExportDataPreservesExistingOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "empty.db")
	outPath := filepath.Join(dir, "export.json")
	if err := os.WriteFile(outPath, []byte("previous export"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := exportData([]string{"--db", "sqlite://" + dbPath, "--out", outPath})
	if err == nil {
		t.Fatal("expected export failure for database without schema")
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "previous export" {
		t.Fatalf("existing export was overwritten: %q", body)
	}
}

func TestCommandDBDefaultsUseEnvironment(t *testing.T) {
	t.Setenv("CLICKCLACK_DATA", "/tmp/clickclack-env-data")
	t.Setenv("CLICKCLACK_DB", "postgres://example.invalid/clickclack")
	t.Setenv("CLICKCLACK_UPLOADS", "r2://bucket/uploads")
	if got := defaultData(); got != "/tmp/clickclack-env-data" {
		t.Fatalf("defaultData = %q", got)
	}
	if got := defaultDB(); got != "postgres://example.invalid/clickclack" {
		t.Fatalf("defaultDB = %q", got)
	}
	if got := defaultUploads(); got != "r2://bucket/uploads" {
		t.Fatalf("defaultUploads = %q", got)
	}
}

func TestOpenUploadStorageValidation(t *testing.T) {
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: "r2://bucket/prod"}); err == nil {
		t.Fatal("expected missing r2 credentials error")
	}
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: "file://" + t.TempDir()}); err != nil {
		t.Fatalf("file upload storage: %v", err)
	}
	if _, err := openUploadStorage(config.Config{Data: t.TempDir(), Uploads: t.TempDir()}); err != nil {
		t.Fatalf("plain upload storage path: %v", err)
	}
}

func TestMessagesListOmitsAfterSeqUntilExplicitlySet(t *testing.T) {
	var messagePaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{{ID: "wsp_1", Slug: "one", Name: "One"}}})
		case "/api/workspaces/wsp_1/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{{ID: "chn_1", WorkspaceID: "wsp_1", Name: "general"}}})
		case "/api/channels/chn_1/messages":
			messagePaths = append(messagePaths, r.URL.RawQuery)
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": []store.Message{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: "wsp_1", Channel: "chn_1", Plain: true}, http: server.Client()}
	if err := c.messagesList([]string{"--limit", "2"}); err != nil {
		t.Fatal(err)
	}
	if len(messagePaths) != 1 {
		t.Fatalf("expected one messages request, got %d", len(messagePaths))
	}
	if strings.Contains(messagePaths[0], "after_seq=") {
		t.Fatalf("unexpected after_seq in default query: %q", messagePaths[0])
	}
	if err := c.messagesList([]string{"--limit", "2", "--after-seq", "4"}); err != nil {
		t.Fatal(err)
	}
	if len(messagePaths) != 2 || !strings.Contains(messagePaths[1], "after_seq=4") {
		t.Fatalf("expected explicit after_seq query, got %v", messagePaths)
	}
}

func TestStatusFailsForExplicitMissingWorkspaceOrChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"user": store.User{ID: "usr_1", DisplayName: "User"}})
		case "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{{ID: "wsp_1", Slug: "one", Name: "One"}}})
		case "/api/workspaces/wsp_1/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{{ID: "chn_1", WorkspaceID: "wsp_1", Name: "general"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: "missing", Plain: true}, http: server.Client()}
	if err := c.status(nil); err == nil || !strings.Contains(err.Error(), `workspace "missing" not found`) {
		t.Fatalf("expected missing workspace error, got %v", err)
	}

	c = apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: "wsp_1", Channel: "missing", Plain: true}, http: server.Client()}
	if err := c.status(nil); err == nil || !strings.Contains(err.Error(), `channel "missing" not found`) {
		t.Fatalf("expected missing channel error, got %v", err)
	}
}
