package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/httpapi"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestThreadOpenPreservesAPIPages(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(t.TempDir(), "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := st.EnsureBootstrap(ctx, "Thread Reader", "reader@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspaces[0].ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: owner.ID, Body: "root"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 201; i++ {
		if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: fmt.Sprintf("reply-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(httpapi.New(st, realtime.NewHub(), httpapi.Options{}).Handler())
	t.Cleanup(server.Close)
	c := apiClient{opts: clientOptions{Server: server.URL, UserID: owner.ID, JSON: true}, http: server.Client()}
	for _, tc := range []struct {
		name, query string
		args        []string
		first, last int64
	}{
		{"earliest", "limit=2", nil, 1, 2},
		{"latest", "limit=2&latest=true", []string{"--latest"}, 200, 201},
		{"before", "limit=2&before_seq=200", []string{"--before-seq", "200"}, 198, 199},
		{"after", "limit=2&after_seq=200", []string{"--after-seq", "200"}, 201, 201},
		{"around", "limit=2&around_seq=200", []string{"--around-seq", "200"}, 200, 201},
		{"zero cursor", "limit=2&before_seq=0", []string{"--before-seq", "0"}, 0, 0},
		{"latest false", "limit=2&latest=false", []string{"--latest=false"}, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want map[string]any
			if err := c.get("/api/messages/"+root.ID+"/thread?"+tc.query, &want); err != nil {
				t.Fatal(err)
			}
			output := captureStdout(t, func() error { return c.threads(append([]string{"open", root.ID, "--limit", "2"}, tc.args...)) })
			var got map[string]any
			if err := json.Unmarshal([]byte(output), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("CLI output differs from API:\n%s\nwant %#v", output, want)
			}
			if got["oldest_seq"] != float64(tc.first) || got["newest_seq"] != float64(tc.last) {
				t.Fatalf("wrong reply window: %v..%v", got["oldest_seq"], got["newest_seq"])
			}
		})
	}
	t.Run("invalid cursors", func(t *testing.T) {
		for _, args := range [][]string{{"--latest", "--after-seq", "1"}, {"--before-seq", "2", "--after-seq", "1"}, {"--after-seq", "-1"}} {
			if err := c.threadOpen(root.ID, args); err == nil || !strings.Contains(err.Error(), "400") {
				t.Fatalf("args %v: want API validation error, got %v", args, err)
			}
		}
	})
}
