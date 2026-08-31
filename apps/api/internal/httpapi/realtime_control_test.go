package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
)

func TestRealtimeIdleConnectionControlFrames(t *testing.T) {
	for _, scenario := range []string{"ping", "disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newRealtimeWorkspaceFixture(t, "control-"+scenario+"@example.com")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			handler := New(fixture.store, realtime.NewHub(), Options{}).Handler()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				handler.ServeHTTP(w, r.WithContext(ctx))
			}))
			defer server.Close()
			conn := dialRealtimeAsUser(t, server.URL, fixture.workspace.ID, fixture.owner.ID)
			defer conn.CloseNow()
			if scenario == "ping" {
				conn.CloseRead(ctx)
				pingCtx, pingCancel := context.WithTimeout(ctx, time.Second)
				defer pingCancel()
				if err := conn.Ping(pingCtx); err != nil {
					t.Fatalf("idle realtime socket did not answer ping: %v", err)
				}
			}
			if err := conn.CloseNow(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("idle realtime handler remained subscribed after peer disconnected")
			}
		})
	}
}
