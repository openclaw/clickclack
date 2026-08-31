package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
)

type latePublicationCreateResult struct {
	Message store.Message `json:"message"`
	Event   store.Event   `json:"event"`
}

type latePublicationHTTPResult struct {
	result latePublicationCreateResult
	err    error
}

type latePublicationStore struct {
	store.Store
	heldNonce string
	committed chan latePublicationCreateResult
	release   <-chan struct{}
	tails     chan string
}

func (s *latePublicationStore) CreateMessage(ctx context.Context, input store.CreateMessageInput) (store.Message, store.Event, error) {
	message, event, err := s.Store.CreateMessage(ctx, input)
	if err != nil || input.Nonce != s.heldNonce {
		return message, event, err
	}
	// The real SQLite transaction has committed. Hold only this request's
	// return to the HTTP handler, where the durable event is published.
	s.committed <- latePublicationCreateResult{Message: message, Event: event}
	select {
	case <-s.release:
		return message, event, nil
	case <-ctx.Done():
		return store.Message{}, store.Event{}, ctx.Err()
	}
}

func (s *latePublicationStore) LatestEventCursor(ctx context.Context, workspaceID, userID string) (string, error) {
	cursor, err := s.Store.LatestEventCursor(ctx, workspaceID, userID)
	if err == nil {
		select {
		case s.tails <- cursor:
		default:
		}
	}
	return cursor, err
}

func TestRealtimeReconnectDoesNotLoseLatePublication(t *testing.T) {
	fixture := newRealtimeWorkspaceFixture(t, "late-publication-owner@example.com")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	_, baseline, err := fixture.store.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: fixture.channel.ID,
		AuthorID:  fixture.owner.ID,
		Body:      "baseline before either request",
	})
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	releaseHeld := sync.OnceFunc(func() { close(release) })
	defer releaseHeld()
	wrapped := &latePublicationStore{
		Store:     fixture.store,
		heldNonce: "late-publication-a",
		committed: make(chan latePublicationCreateResult, 1),
		release:   release,
		tails:     make(chan string, 2),
	}
	server := httptest.NewServer(New(wrapped, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)

	post := func(body, nonce string) (latePublicationCreateResult, error) {
		var result latePublicationCreateResult
		payload, err := json.Marshal(map[string]string{"body": body, "nonce": nonce})
		if err != nil {
			return result, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			server.URL+"/api/channels/"+fixture.channel.ID+"/messages", bytes.NewReader(payload))
		if err != nil {
			return result, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ClickClack-User", fixture.owner.ID)
		resp, err := server.Client().Do(req)
		if err != nil {
			return result, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			return result, fmt.Errorf("create message returned %s", resp.Status)
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return result, err
		}
		return result, nil
	}
	mustPost := func(body, nonce string) latePublicationCreateResult {
		t.Helper()
		result, err := post(body, nonce)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	dial := func(cursor string) *websocket.Conn {
		t.Helper()
		endpoint := strings.Replace(server.URL, "http://", "ws://", 1) +
			"/api/realtime/ws?workspace_id=" + url.QueryEscape(fixture.workspace.ID) +
			"&after_cursor=" + url.QueryEscape(cursor)
		conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
			HTTPHeader: http.Header{"X-ClickClack-User": []string{fixture.owner.ID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		select {
		case captured := <-wrapped.tails:
			if captured != cursor {
				t.Fatalf("captured tail = %q, want starting checkpoint %q", captured, cursor)
			}
		case <-ctx.Done():
			t.Fatalf("websocket did not capture its tail: %v", ctx.Err())
		}
		return conn
	}
	read := func(conn *websocket.Conn) store.Event {
		t.Helper()
		_, payload, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read real event: %v", err)
		}
		var event store.Event
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		return event
	}

	first := dial(baseline.Cursor)
	aResponse := make(chan latePublicationHTTPResult, 1)
	go func() {
		result, err := post("A commits before B but its handler is held", wrapped.heldNonce)
		aResponse <- latePublicationHTTPResult{result: result, err: err}
	}()
	var a latePublicationCreateResult
	select {
	case a = <-wrapped.committed:
	case response := <-aResponse:
		t.Fatalf("A returned before reaching the commit barrier: %v", response.err)
	case <-ctx.Done():
		t.Fatalf("A did not reach the commit barrier: %v", ctx.Err())
	}
	persisted, err := fixture.store.GetMessage(ctx, a.Message.ID, fixture.owner.ID)
	if err != nil || persisted.Body != a.Message.Body {
		t.Fatalf("A is not durably readable while its handler is held: message=%#v error=%v", persisted, err)
	}

	b := mustPost("B publishes while A's handler is held", "late-publication-b")
	if a.Event.Cursor == "" || a.Event.Cursor >= b.Event.Cursor {
		t.Fatalf("fixture cursors do not establish A before B: A=%q B=%q", a.Event.Cursor, b.Event.Cursor)
	}
	seenA := 0
	var checkpoint string
	for {
		event := read(first)
		if event.ID == a.Event.ID {
			seenA++
		}
		if event.ID == b.Event.ID {
			checkpoint = event.Cursor
			break
		}
	}
	if err := first.CloseNow(); err != nil {
		t.Fatal(err)
	}

	reconnected := dial(checkpoint)
	releaseHeld()
	select {
	case response := <-aResponse:
		if response.err != nil || response.result.Event.ID != a.Event.ID {
			t.Fatalf("held A request failed: result=%#v error=%v", response.result, response.err)
		}
	case <-ctx.Done():
		t.Fatalf("A did not finish after release: %v", ctx.Err())
	}

	// A's completed HTTP response establishes that its publication finished.
	// A later real message is the delivery barrier; no timeout proves absence.
	sentinel := mustPost("sentinel after A's response completed", "late-publication-sentinel")
	for {
		event := read(reconnected)
		if event.ID == a.Event.ID {
			seenA++
		}
		if event.ID == sentinel.Event.ID {
			break
		}
	}
	if seenA != 1 {
		t.Fatalf("committed event A was delivered %d times across checkpoint and reconnect; want once: A=%s checkpoint=%s sentinel=%s", seenA, a.Event.Cursor, checkpoint, sentinel.Event.Cursor)
	}
}

func TestPrivatePublicationWakesEarlierPublicEvent(t *testing.T) {
	f := newRealtimeWorkspaceFixture(t, "private-wake@example.com")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	viewer, err := f.store.CreateUser(ctx, store.CreateUserInput{DisplayName: "Viewer", Email: "private-viewer@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.AddWorkspaceMember(ctx, f.workspace.ID, viewer.ID, "member"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	observed := &capturedTailStore{Store: f.store, captured: make(chan string, 1), release: release}
	server := httptest.NewServer(New(observed, realtime.NewHub(), Options{}).Handler())
	defer server.Close()
	conn := dialRealtimeAsUser(t, server.URL, f.workspace.ID, viewer.ID)
	defer conn.CloseNow()
	select {
	case <-observed.captured:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Commit A without publishing its receipt; the later private read is its sole wakeup.
	message, a, err := f.store.CreateMessage(ctx, store.CreateMessageInput{ChannelID: f.channel.ID, AuthorID: f.owner.ID, Body: "earlier public event"})
	if err != nil {
		t.Fatal(err)
	}
	postJSONAsUser[map[string]any](t, f.owner.ID, server.URL+"/api/channels/"+f.channel.ID+"/read", map[string]int64{"seq": *message.ChannelSeq})
	releaseOnce()
	_, body, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("private publication did not wake public event: %v", err)
	}
	var got store.Event
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID {
		t.Fatalf("got %#v, want only earlier public event %s", got, a.ID)
	}
}
