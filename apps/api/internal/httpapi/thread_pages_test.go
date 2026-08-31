package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestThreadLatestWindowHTTP(t *testing.T) {
	t.Parallel()
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
	owner, err := st.EnsureBootstrap(ctx, "Thread Owner", "thread-http-owner@example.com")
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
	for index := 1; index <= 5; index++ {
		if _, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
			RootMessageID: root.ID,
			AuthorID:      owner.ID,
			Body:          fmt.Sprintf("reply-%d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	thread := getJSON[struct {
		Replies     []store.Message   `json:"replies"`
		ThreadState store.ThreadState `json:"thread_state"`
	}](t, server.URL+"/api/messages/"+url.PathEscape(root.ID)+"/thread?latest=true&limit=2")
	if thread.ThreadState.ReplyCount != 5 || len(thread.Replies) != 2 || thread.Replies[0].Body != "reply-4" || thread.Replies[1].Body != "reply-5" {
		t.Fatalf("unexpected latest HTTP thread window: %#v", thread)
	}
	for _, tc := range []struct {
		query        string
		first, last  int64
		older, newer bool
	}{
		{"limit=2", 1, 2, false, true}, {"latest=true&limit=2", 4, 5, true, false},
		{"before_seq=4&limit=2", 2, 3, true, true}, {"after_seq=3&limit=2", 4, 5, true, false},
		{"around_seq=3&limit=3", 2, 4, true, true}, {"around_seq=0&limit=3", 1, 3, false, true},
		{"around_seq=99&limit=3", 3, 5, true, false}, {"after_seq=5", 0, 0, false, false}, {"before_seq=1", 0, 0, false, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := getJSON[struct {
				Replies []store.Message `json:"replies"`
				Oldest  int64           `json:"oldest_seq"`
				Newest  int64           `json:"newest_seq"`
				Older   bool            `json:"has_older"`
				Newer   bool            `json:"has_newer"`
			}](t, server.URL+"/api/messages/"+root.ID+"/thread?"+tc.query)
			if got.Oldest != tc.first || got.Newest != tc.last || got.Older != tc.older || got.Newer != tc.newer {
				t.Fatalf("unexpected thread edges: %#v", got)
			}
			if tc.first != 0 && (*got.Replies[0].ThreadSeq != tc.first || *got.Replies[len(got.Replies)-1].ThreadSeq != tc.last) {
				t.Fatalf("wrong reply interval: %#v", got.Replies)
			}
		})
	}
	for _, query := range []string{"before_seq=-1", "after_seq=no", "around_seq=", "before_seq=2&after_seq=1", "latest=true&after_seq=1"} {
		expectStatus(t, http.MethodGet, server.URL+"/api/messages/"+root.ID+"/thread?"+query, nil, http.StatusBadRequest)
	}

	expectStatus(t, http.MethodGet, server.URL+"/api/messages/"+url.PathEscape(root.ID)+"/thread?latest=maybe", nil, http.StatusBadRequest)
}
