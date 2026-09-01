package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

type realtimeSessionFixture struct {
	store     *sqlitestore.Store
	hub       *realtime.Hub
	server    *Server
	http      *httptest.Server
	owner     store.User
	workspace store.Workspace
}

// newRealtimeSessionFixture gives one password-enrolled account a workspace and
// a running server, so a test can open a socket on one session and revoke it
// from another. A non-zero sessionRecheck shortens the idle revalidation
// interval, and is applied before the server starts serving.
func newRealtimeSessionFixture(t *testing.T, email string, sessionRecheck time.Duration) realtimeSessionFixture {
	t.Helper()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", email)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, owner.ID, hashFor(t, passwordTestSecret)); err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	server := New(st, hub, Options{PasswordAuthEnabled: true})
	if sessionRecheck > 0 {
		server.realtimeSessionCheck = sessionRecheck
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return realtimeSessionFixture{
		store:     st,
		hub:       hub,
		server:    server,
		http:      httpServer,
		owner:     owner,
		workspace: workspaces[0],
	}
}

func dialRealtimeWithSession(t *testing.T, serverURL, workspaceID, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1) + "/api/realtime/ws?workspace_id=" + url.QueryEscape(workspaceID)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{websocketBearerProtocolPrefix + token},
	})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// expectRealtimeSessionClose reads until the socket closes, and fails if it
// delivers anything first. A revoked session must not receive another event.
func expectRealtimeSessionClose(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, body, err := conn.Read(ctx)
	if err == nil {
		t.Fatalf("revoked session still received an event: %s", body)
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("close status = %v, want %v: %v", got, websocket.StatusPolicyViolation, err)
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Reason != realtimeSessionRevokedCloseReason {
		t.Fatalf("close error = %#v, want reason %q", err, realtimeSessionRevokedCloseReason)
	}
}

func TestRealtimeConnectionStopsWhenItsSessionIsRevoked(t *testing.T) {
	for _, scenario := range []string{"password-change", "logout"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			fixture := newRealtimeSessionFixture(t, "realtime-session-"+scenario+"@example.com", 0)
			// The socket's own session, and a second device that outlives it.
			connected, err := fixture.store.CreateSession(ctx, fixture.owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			elsewhere, err := fixture.store.CreateSession(ctx, fixture.owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			conn := dialRealtimeWithSession(t, fixture.http.URL, fixture.workspace.ID, connected.Token)
			defer conn.CloseNow()

			// Positive control: the socket is live and delivering before anything
			// is revoked.
			fixture.hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
			if _, ok := readEventTypeWithin(t, conn, "presence.changed", 5*time.Second); !ok {
				t.Fatal("expected the open socket to deliver before revocation")
			}

			switch scenario {
			case "password-change":
				resp, body := changePasswordRequest(t, fixture.http.URL, passwordTestSecret, changedPasswordSecret,
					withBearer(elsewhere.Token), nil)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("expected the change to succeed, got %d %s", resp.StatusCode, body)
				}
			case "logout":
				if resp := logoutRequest(t, fixture.http.URL, withBearer(connected.Token)); resp.StatusCode != http.StatusOK {
					t.Fatalf("expected sign-out to succeed, got %d", resp.StatusCode)
				}
			}
			if _, err := fixture.store.GetSessionUser(ctx, connected.Token); err == nil {
				t.Fatal("expected the connected session to be revoked in the database")
			}

			// The database revocation has to reach the socket that was already
			// open when it happened, not just the next request that authenticates.
			fixture.hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
			expectRealtimeSessionClose(t, conn)
		})
	}
}

func TestRealtimeIdleConnectionClosesAfterRevocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Delivery revalidates the session, so a quiet workspace is the case that
	// needs the periodic recheck. Shorten it rather than waiting for the
	// production interval.
	fixture := newRealtimeSessionFixture(t, "realtime-session-idle@example.com", 20*time.Millisecond)
	connected, err := fixture.store.CreateSession(ctx, fixture.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn := dialRealtimeWithSession(t, fixture.http.URL, fixture.workspace.ID, connected.Token)
	defer conn.CloseNow()

	fixture.hub.Publish(store.Event{WorkspaceID: fixture.workspace.ID, Type: "presence.changed"})
	if _, ok := readEventTypeWithin(t, conn, "presence.changed", 5*time.Second); !ok {
		t.Fatal("expected the open socket to deliver before revocation")
	}
	if err := fixture.store.RevokeSession(ctx, connected.Token); err != nil {
		t.Fatal(err)
	}
	// Nothing is published here: the socket has to notice on its own.
	expectRealtimeSessionClose(t, conn)
}
