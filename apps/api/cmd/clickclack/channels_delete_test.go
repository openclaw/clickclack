package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type channelDeletionServer struct {
	*httptest.Server
	blocker string
	deletes []string
}

func newChannelDeletionServer(t *testing.T) *channelDeletionServer {
	t.Helper()
	fake := &channelDeletionServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/workspaces":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": []store.Workspace{{ID: "wsp_1", Slug: "one", Name: "One"}}})
		case r.URL.Path == "/api/workspaces/wsp_1/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{"channels": []store.Channel{
				{ID: "chn_1", WorkspaceID: "wsp_1", Name: "general"},
				{ID: "chn_2", WorkspaceID: "wsp_1", Name: "doomed"},
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/channels/chn_2/deletion-preview":
			_ = json.NewEncoder(w).Encode(store.ChannelDeletionPreview{
				Channel: store.Channel{ID: "chn_2", Name: "doomed"},
				Counts:  store.ChannelDeletionCounts{Messages: 12, ThreadReplies: 3, Files: 2},
				Blocker: fake.blocker,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/channels/chn_2":
			fake.deletes = append(fake.deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func TestChannelsDeleteRequiresExplicitConfirmation(t *testing.T) {
	server := newChannelDeletionServer(t)
	c := apiClient{opts: clientOptions{Server: server.URL, UserID: "usr_1", Workspace: "wsp_1"}, http: server.Client()}

	if err := c.channels([]string{"delete"}); err == nil || !strings.Contains(err.Error(), "--channel CHANNEL --yes") {
		t.Fatalf("expected an explicit channel requirement, got %v", err)
	}
	if err := c.channels([]string{"delete", "--channel", "doomed"}); err == nil || !strings.Contains(err.Error(), "#doomed: 12 messages, 3 thread replies, 2 files without --yes") {
		t.Fatalf("expected a confirmation requirement, got %v", err)
	}
	server.blocker = store.ChannelDeletionBlockedLastChannel
	if err := c.channels([]string{"delete", "--channel", "doomed", "--yes"}); err == nil || !strings.Contains(err.Error(), "(last_channel)") {
		t.Fatalf("expected the blocker to stop deletion, got %v", err)
	}
	if len(server.deletes) != 0 {
		t.Fatalf("unconfirmed or blocked commands sent deletes: %v", server.deletes)
	}
	server.blocker = ""
	output := captureStdout(t, func() error {
		return c.channels([]string{"delete", "--channel", "doomed", "--yes"})
	})
	if len(server.deletes) != 1 || output != "deleted #doomed: 12 messages, 3 thread replies, 2 files\n" {
		t.Fatalf("deletes=%v output=%q", server.deletes, output)
	}
}

func TestChannelsDeleteIgnoresDefaultChannels(t *testing.T) {
	server := newChannelDeletionServer(t)
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("CLICKCLACK_SERVER", server.URL)
	t.Setenv("CLICKCLACK_USER_ID", "usr_1")
	t.Setenv("CLICKCLACK_WORKSPACE", "wsp_1")

	t.Setenv("CLICKCLACK_CHANNEL", "doomed")
	if err := client([]string{"channels", "delete", "--yes"}); err == nil || !strings.Contains(err.Error(), "--channel CHANNEL --yes") {
		t.Fatalf("CLICKCLACK_CHANNEL chose the channel to delete: %v", err)
	}
	t.Setenv("CLICKCLACK_CHANNEL", "")
	config, err := json.Marshal(clientConfig{Server: server.URL, Workspace: "wsp_1", Channel: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configHome, "clickclack"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "clickclack", "config.json"), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client([]string{"channels", "delete", "--yes"}); err == nil || !strings.Contains(err.Error(), "--channel CHANNEL --yes") {
		t.Fatalf("the saved default channel chose the channel to delete: %v", err)
	}
	if len(server.deletes) != 0 {
		t.Fatalf("a default channel was deleted: %v", server.deletes)
	}

	for _, args := range [][]string{
		{"--channel", "doomed", "channels", "delete", "--yes"},
		{"channels", "delete", "--channel", "doomed", "--yes"},
	} {
		captureStdout(t, func() error { return client(args) })
	}
	if len(server.deletes) != 2 {
		t.Fatalf("named channels were not deleted: %v", server.deletes)
	}
}
