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
	expectStatus(t, http.MethodGet, server.URL+"/api/messages/"+url.PathEscape(root.ID)+"/thread?latest=maybe", nil, http.StatusBadRequest)
}
