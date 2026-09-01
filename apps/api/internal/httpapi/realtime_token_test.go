package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func createRealtimeBot(t *testing.T, fixture realtimeWorkspaceFixture) (store.BotToken, store.CreateBotInput) {
	t.Helper()
	input := store.CreateBotInput{
		WorkspaceID: fixture.workspace.ID, DisplayName: "Realtime bot",
		CreatedBy: fixture.owner.ID, Scopes: []string{"bot:read"}, SetupNonce: "realtime-token-proof",
	}
	_, token, err := fixture.store.CreateBot(fixture.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return token, input
}

func expectRealtimeTokenClose(t *testing.T, conn *websocket.Conn, code websocket.StatusCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if got := websocket.CloseStatus(err); got != code {
		t.Fatalf("credential check returned %v, want close %v: %v", got, code, err)
	}
}

func TestRealtimeBotTokenRevocation(t *testing.T) {
	for _, scenario := range []string{"durable", "ephemeral", "idle", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			fixture := newRealtimeWorkspaceFixture(t, scenario+"-token@example.com")
			token, input := createRealtimeBot(t, fixture)
			hub := realtime.NewHub()
			server := New(fixture.store, hub, Options{})
			if scenario == "idle" {
				server.realtimeSessionCheck = 20 * time.Millisecond
			}
			httpServer := httptest.NewServer(server.Handler())
			defer httpServer.Close()
			conn := dialRealtimeWithSession(t, httpServer.URL, fixture.workspace.ID, token.Token)
			defer conn.CloseNow()
			hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
			if _, ok := readEventTypeWithin(t, conn, "presence.changed", 5*time.Second); !ok {
				t.Fatal("bot did not receive the positive control")
			}

			var replacement store.BotToken
			if scenario == "replacement" {
				_, rotated, err := fixture.store.CreateBot(fixture.ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				if rotated.ID != token.ID || rotated.Token == token.Token {
					t.Fatal("setup replay must replace the credential in the same token row")
				}
				replacement = rotated
			} else if _, err := fixture.store.RevokeBotToken(fixture.ctx, token.ID, fixture.owner.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.store.GetBotTokenAuth(fixture.ctx, token.Token); err == nil {
				t.Fatal("old credential still authenticates")
			}
			switch scenario {
			case "durable":
				_, event, err := fixture.store.CreateMessage(fixture.ctx, store.CreateMessageInput{
					ChannelID: fixture.channel.ID, AuthorID: fixture.owner.ID, Body: "After revocation",
				})
				if err != nil {
					t.Fatal(err)
				}
				hub.Publish(event)
			case "ephemeral", "replacement":
				hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
			}
			expectRealtimeTokenClose(t, conn, websocket.StatusPolicyViolation)
			if replacement.Token != "" {
				current := dialRealtimeWithSession(t, httpServer.URL, fixture.workspace.ID, replacement.Token)
				defer current.CloseNow()
				hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
				if _, ok := readEventTypeWithin(t, current, "presence.changed", 5*time.Second); !ok {
					t.Fatal("replacement credential could not receive events")
				}
			}
		})
	}
}

func TestRealtimeReplayRechecksBotTokenAfterVisibilityLookup(t *testing.T) {
	fixture := newRealtimeWorkspaceFixture(t, "replay-token@example.com")
	token, _ := createRealtimeBot(t, fixture)
	if _, _, err := fixture.store.CreateMessage(fixture.ctx, store.CreateMessageInput{
		ChannelID: fixture.channel.ID, AuthorID: fixture.owner.ID, Body: "Before connection",
	}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	blocked := &blockingChannelStore{Store: fixture.store, entered: make(chan struct{}), release: release, tailCaptured: make(chan struct{})}
	httpServer := httptest.NewServer(New(blocked, realtime.NewHub(), Options{}).Handler())
	defer httpServer.Close()
	conn := dialRealtimeWithSession(t, httpServer.URL, fixture.workspace.ID, token.Token)
	defer conn.CloseNow()
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("replay did not reach visibility lookup")
	}
	if _, err := fixture.store.RevokeBotToken(fixture.ctx, token.ID, fixture.owner.ID); err != nil {
		t.Fatal(err)
	}
	unblock()
	expectRealtimeTokenClose(t, conn, websocket.StatusPolicyViolation)
}

type unavailableBotTokenStore struct {
	store.Store
	unavailable atomic.Bool
}

func (s *unavailableBotTokenStore) GetBotTokenAuth(ctx context.Context, token string) (store.BotTokenAuth, error) {
	if s.unavailable.Load() {
		return store.BotTokenAuth{}, errors.New("temporary database failure")
	}
	return s.Store.GetBotTokenAuth(ctx, token)
}

func TestRealtimeBotTokenLookupFailureRemainsRetryable(t *testing.T) {
	fixture := newRealtimeWorkspaceFixture(t, "retry-token@example.com")
	token, _ := createRealtimeBot(t, fixture)
	wrapped := &unavailableBotTokenStore{Store: fixture.store}
	hub := realtime.NewHub()
	httpServer := httptest.NewServer(New(wrapped, hub, Options{}).Handler())
	defer httpServer.Close()
	conn := dialRealtimeWithSession(t, httpServer.URL, fixture.workspace.ID, token.Token)
	defer conn.CloseNow()
	hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
	if _, ok := readEventTypeWithin(t, conn, "presence.changed", 5*time.Second); !ok {
		t.Fatal("bot did not receive the positive control")
	}
	wrapped.unavailable.Store(true)
	hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
	expectRealtimeTokenClose(t, conn, websocket.StatusTryAgainLater)
	wrapped.unavailable.Store(false)
	current := dialRealtimeWithSession(t, httpServer.URL, fixture.workspace.ID, token.Token)
	defer current.CloseNow()
	hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
	if _, ok := readEventTypeWithin(t, current, "presence.changed", 5*time.Second); !ok {
		t.Fatal("credential could not recover after the store recovered")
	}
}
