package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type blockingChannelStore struct {
	store.Store
	tailCaptured chan struct{}
	entered      chan struct{}
	release      <-chan struct{}
	tailOnce     sync.Once
	once         sync.Once
}

type capturedTailStore struct {
	store.Store
	captured chan string
	release  <-chan struct{}
	once     sync.Once
}

type smallReplayPageStore struct {
	store.Store
}

type emptyReplayPageStore struct {
	store.Store
}

type pruneAfterReplayQueryStore struct {
	store.Store
	sqlite      *sqlitestore.Store
	workspaceID string
	once        sync.Once
}

func (s *pruneAfterReplayQueryStore) ListEventsAfter(ctx context.Context, workspaceID, userID, cursor string, limit int) ([]store.Event, error) {
	events, err := s.Store.ListEventsAfter(ctx, workspaceID, userID, cursor, limit)
	if err == nil && cursor != "" {
		s.once.Do(func() {
			_, err = s.sqlite.PruneEvents(ctx, s.workspaceID, 1, "")
		})
	}
	return events, err
}

func (s *emptyReplayPageStore) ListEventsAfter(context.Context, string, string, string, int) ([]store.Event, error) {
	return nil, nil
}

type realtimeWorkspaceFixture struct {
	ctx       context.Context
	store     *sqlitestore.Store
	owner     store.User
	workspace store.Workspace
	channel   store.Channel
}

func newRealtimeWorkspaceFixture(t *testing.T, email string) realtimeWorkspaceFixture {
	t.Helper()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", email)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	return realtimeWorkspaceFixture{
		ctx:       ctx,
		store:     st,
		owner:     owner,
		workspace: workspace,
		channel:   channels[0],
	}
}

func (s *smallReplayPageStore) ListEventsAfter(ctx context.Context, workspaceID, userID, cursor string, _ int) ([]store.Event, error) {
	return s.Store.ListEventsAfter(ctx, workspaceID, userID, cursor, 2)
}

func (s *blockingChannelStore) LatestEventCursor(ctx context.Context, workspaceID, userID string) (string, error) {
	cursor, err := s.Store.LatestEventCursor(ctx, workspaceID, userID)
	if err == nil {
		s.tailOnce.Do(func() { close(s.tailCaptured) })
	}
	return cursor, err
}

func (s *blockingChannelStore) GetChannel(ctx context.Context, channelID, userID string) (store.Channel, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.Store.GetChannel(ctx, channelID, userID)
}

func (s *capturedTailStore) LatestEventCursor(ctx context.Context, workspaceID, userID string) (string, error) {
	cursor, err := s.Store.LatestEventCursor(ctx, workspaceID, userID)
	if err != nil {
		return "", err
	}
	s.once.Do(func() {
		s.captured <- cursor
		<-s.release
	})
	return cursor, nil
}

func TestEventTailCursorUsesVisibleEventWindow(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-tail-owner@example.com")
	ctx, st, owner, workspace, channel := fixture.ctx, fixture.store, fixture.owner, fixture.workspace, fixture.channel
	member, err := st.CreateUser(ctx, store.CreateUserInput{
		DisplayName: "Member",
		Email:       "realtime-tail-member@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, "member"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	first := postJSONAsUser[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{
		"body": "first",
	})
	postJSONAsUser[struct {
		Receipt store.ReadReceipt `json:"receipt"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/read", map[string]int64{
		"seq": *first.Message.ChannelSeq,
	})
	second := postJSONAsUser[struct {
		Message store.Message `json:"message"`
		Event   store.Event   `json:"event"`
	}](t, owner.ID, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{
		"body": "second",
	})

	result := getJSONAsUser[struct {
		Events     []store.Event `json:"events"`
		TailCursor string        `json:"tail_cursor"`
	}](t, member.ID, server.URL+"/api/realtime/events?workspace_id="+
		url.QueryEscape(workspace.ID)+"&after_cursor="+url.QueryEscape(first.Event.Cursor)+
		"&limit=1&include_tail=true")

	if len(result.Events) != 1 || result.Events[0].ID != second.Event.ID {
		t.Fatalf("hidden read receipt consumed the visible page: %#v", result.Events)
	}
	if result.TailCursor != second.Event.Cursor {
		t.Fatalf("tail cursor = %q, want %q", result.TailCursor, second.Event.Cursor)
	}
}

func TestRealtimeConnectPagesToCapturedTailWithoutDuplicatingLiveEvent(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-paged-owner@example.com")
	ctx, st, owner, workspace, channel := fixture.ctx, fixture.store, fixture.owner, fixture.workspace, fixture.channel

	const backlogSize = 3
	backlog := make([]store.Event, 0, backlogSize)
	for i := 0; i < backlogSize; i++ {
		_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("paged replay message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		backlog = append(backlog, event)
	}

	hub := realtime.NewHub()
	captured := make(chan string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	tailStore := &capturedTailStore{
		Store:    &smallReplayPageStore{Store: st},
		captured: captured,
		release:  release,
	}
	server := httptest.NewServer(New(tailStore, hub, Options{}).Handler())
	t.Cleanup(server.Close)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID)
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{owner.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	select {
	case got := <-captured:
		if want := backlog[len(backlog)-1].Cursor; got != want {
			t.Fatalf("captured tail = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not capture the replay tail")
	}
	_, postSubscribe, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "created after replay tail capture",
	})
	if err != nil {
		t.Fatal(err)
	}
	hub.Publish(postSubscribe)
	releaseOnce.Do(func() { close(release) })

	readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer readCancel()
	counts := make(map[string]int, backlogSize+1)
	for i := 0; i < backlogSize+1; i++ {
		_, body, err := conn.Read(readCtx)
		if err != nil {
			t.Fatal(err)
		}
		var event store.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		counts[event.ID]++
	}
	for _, event := range backlog {
		if counts[event.ID] != 1 {
			t.Fatalf("backlog event %q delivered %d times, want once", event.ID, counts[event.ID])
		}
	}
	if counts[postSubscribe.ID] != 1 {
		t.Fatalf("post-subscribe event delivered %d times, want once", counts[postSubscribe.ID])
	}

	sentinel := store.Event{WorkspaceID: workspace.ID, Type: "presence.changed"}
	hub.Publish(sentinel)
	_, body, err := conn.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	var next store.Event
	if err := json.Unmarshal(body, &next); err != nil {
		t.Fatal(err)
	}
	if next.Type != sentinel.Type || next.Cursor != "" {
		t.Fatalf("next live event = %#v, want ephemeral sentinel without a queued duplicate", next)
	}
}

func TestRealtimePruneRaceDuringFirstPageRequestsAuthoritativeResync(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-prune-race-owner@example.com")
	created := make([]store.Event, 0, 3)
	for i := 0; i < 3; i++ {
		_, event, err := fixture.store.CreateMessage(fixture.ctx, store.CreateMessageInput{
			ChannelID: fixture.channel.ID,
			AuthorID:  fixture.owner.ID,
			Body:      fmt.Sprintf("prune race message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, event)
	}

	wrapped := &pruneAfterReplayQueryStore{
		Store:       fixture.store,
		sqlite:      fixture.store,
		workspaceID: fixture.workspace.ID,
	}
	server := httptest.NewServer(New(wrapped, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/realtime/ws?workspace_id=" + url.QueryEscape(fixture.workspace.ID) +
		"&after_cursor=" + url.QueryEscape(created[0].Cursor)
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{fixture.owner.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(dialCtx)
	if got := websocket.CloseStatus(err); got != realtimeResyncRequiredStatus {
		t.Fatalf("close status = %v, want %v: %v", got, realtimeResyncRequiredStatus, err)
	}
}

func TestRealtimePrunedCursorWithNewerEventsRequestsAuthoritativeResync(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-pruned-gap-owner@example.com")
	created := make([]store.Event, 0, 3)
	for i := 0; i < 3; i++ {
		_, event, err := fixture.store.CreateMessage(fixture.ctx, store.CreateMessageInput{
			ChannelID: fixture.channel.ID,
			AuthorID:  fixture.owner.ID,
			Body:      fmt.Sprintf("pruned gap message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, event)
	}
	if _, err := fixture.store.PruneEvents(fixture.ctx, fixture.workspace.ID, 1, ""); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(fixture.store, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/realtime/ws?workspace_id=" + url.QueryEscape(fixture.workspace.ID) +
		"&after_cursor=" + url.QueryEscape(created[0].Cursor)
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{fixture.owner.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(dialCtx)
	if got := websocket.CloseStatus(err); got != realtimeResyncRequiredStatus {
		t.Fatalf("close status = %v, want %v: %v", got, realtimeResyncRequiredStatus, err)
	}
}

func TestRealtimeEmptyReplayPageRequestsAuthoritativeResync(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-pruned-owner@example.com")
	_, _, err := fixture.store.CreateMessage(fixture.ctx, store.CreateMessageInput{
		ChannelID: fixture.channel.ID,
		AuthorID:  fixture.owner.ID,
		Body:      "retained tail hidden by replay gap",
	})
	if err != nil {
		t.Fatal(err)
	}

	wrapped := &emptyReplayPageStore{Store: fixture.store}
	server := httptest.NewServer(New(wrapped, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	conn := dialRealtimeAsUser(t, server.URL, fixture.workspace.ID, fixture.owner.ID)
	defer conn.CloseNow()

	readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err = conn.Read(readCtx)
	if got := websocket.CloseStatus(err); got != realtimeResyncRequiredStatus {
		t.Fatalf("close status = %v, want %v: %v", got, realtimeResyncRequiredStatus, err)
	}
}

func TestRealtimeReplayLimitRequestsAuthoritativeResync(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-limit-owner@example.com")
	ctx, st, owner, workspace, channel := fixture.ctx, fixture.store, fixture.owner, fixture.workspace, fixture.channel

	created := make([]store.Event, 0, 3)
	for i := 0; i < 3; i++ {
		_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("limited replay message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, event)
	}

	server := New(st, realtime.NewHub(), Options{})
	server.realtimeReplayLimit = 2
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	conn := dialRealtimeAsUser(t, httpServer.URL, workspace.ID, owner.ID)
	defer conn.CloseNow()

	readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		_, body, err := conn.Read(readCtx)
		if err != nil {
			t.Fatal(err)
		}
		var event store.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != created[i].ID {
			t.Fatalf("replayed event %d = %q, want %q", i, event.ID, created[i].ID)
		}
	}
	_, _, err := conn.Read(readCtx)
	if got := websocket.CloseStatus(err); got != realtimeResyncRequiredStatus {
		t.Fatalf("close status = %v, want %v: %v", got, realtimeResyncRequiredStatus, err)
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Reason != realtimeResyncRequiredCloseReason {
		t.Fatalf("close error = %#v, want reason %q", err, realtimeResyncRequiredCloseReason)
	}
}

func TestRealtimeCursorAheadOfTailRequestsAuthoritativeResync(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-ahead-owner@example.com")
	server := httptest.NewServer(New(fixture.store, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
		"/api/realtime/ws?workspace_id=" + url.QueryEscape(fixture.workspace.ID) +
		"&after_cursor=" + url.QueryEscape("zzzz-invalid-future-cursor")
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ClickClack-User": []string{fixture.owner.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	_, _, err = conn.Read(dialCtx)
	if got := websocket.CloseStatus(err); got != realtimeResyncRequiredStatus {
		t.Fatalf("close status = %v, want %v: %v", got, realtimeResyncRequiredStatus, err)
	}
}

func TestRealtimeOverflowClosesAndReconnectReplays(t *testing.T) {
	t.Parallel()
	fixture := newRealtimeWorkspaceFixture(t, "realtime-overflow-owner@example.com")
	ctx, st, owner, workspace, channel := fixture.ctx, fixture.store, fixture.owner, fixture.workspace, fixture.channel

	hub := realtime.NewHub()
	tailCaptured := make(chan struct{})
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	blockingStore := &blockingChannelStore{
		Store:        st,
		tailCaptured: tailCaptured,
		entered:      entered,
		release:      release,
	}
	server := httptest.NewServer(New(blockingStore, hub, Options{}).Handler())
	t.Cleanup(server.Close)

	dial := func(afterCursor string) *websocket.Conn {
		t.Helper()
		dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		wsURL := strings.Replace(server.URL, "http://", "ws://", 1) +
			"/api/realtime/ws?workspace_id=" + url.QueryEscape(workspace.ID) +
			"&after_cursor=" + url.QueryEscape(afterCursor)
		conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
			HTTPHeader: http.Header{"X-ClickClack-User": []string{owner.ID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}

	conn := dial("")
	t.Cleanup(func() { conn.CloseNow() })
	select {
	case <-tailCaptured:
	case <-time.After(5 * time.Second):
		t.Fatal("websocket did not capture the replay tail")
	}

	published := make([]store.Event, 0, 34)
	for i := 0; i < 34; i++ {
		_, event, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      fmt.Sprintf("overflow message %d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		published = append(published, event)
		hub.Publish(event)
		if i == 0 {
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("websocket did not begin delivering the first live event")
			}
		}
	}
	releaseOnce.Do(func() { close(release) })

	readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	received := make([]store.Event, 0, 33)
	for {
		_, body, err := conn.Read(readCtx)
		if err != nil {
			if got := websocket.CloseStatus(err); got != websocket.StatusTryAgainLater {
				t.Fatalf("close status = %v, want %v: %v", got, websocket.StatusTryAgainLater, err)
			}
			var closeErr websocket.CloseError
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected websocket close error, got %T: %v", err, err)
			}
			if closeErr.Reason != realtimeOverflowCloseReason {
				t.Fatalf("close reason = %q, want %q", closeErr.Reason, realtimeOverflowCloseReason)
			}
			break
		}
		var event store.Event
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		received = append(received, event)
	}
	if len(received) == 0 || len(received) >= len(published) {
		t.Fatalf("received %d of %d events before overflow close", len(received), len(published))
	}
	for i, event := range received {
		if event.ID != published[i].ID {
			t.Fatalf("live event %d = %q, want %q", i, event.ID, published[i].ID)
		}
	}

	reconnected := dial(received[len(received)-1].Cursor)
	defer reconnected.CloseNow()
	replayCtx, replayCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer replayCancel()
	for i := len(received); i < len(published); i++ {
		_, body, err := reconnected.Read(replayCtx)
		if err != nil {
			t.Fatal(err)
		}
		var replayed store.Event
		if err := json.Unmarshal(body, &replayed); err != nil {
			t.Fatal(err)
		}
		if replayed.ID != published[i].ID {
			t.Fatalf("replayed event %d = %q, want %q", i, replayed.ID, published[i].ID)
		}
	}
}
