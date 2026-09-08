package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestAtomicMessageReplayPreservesAttachments(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var st store.Store
			if backend == "postgres" {
				st, _ = newIsolatedPostgresHTTPTestStore(t)
			} else {
				st = newEmptyHTTPStore(t)
			}
			ctx := context.Background()
			if err := st.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			owner, err := st.EnsureBootstrap(ctx, "Attachment Owner", "attachments@example.com")
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
			peer, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Peer", Email: "peer@example.com"})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.AddWorkspaceMember(ctx, workspace.ID, peer.ID, store.WorkspaceRoleMember); err != nil {
				t.Fatal(err)
			}
			dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: owner.ID, MemberIDs: []string{peer.ID}})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
			t.Cleanup(server.Close)
			for _, target := range []struct{ kind, path string }{
				{"channel", "/api/channels/" + channels[0].ID + "/messages"},
				{"dm", "/api/dms/" + dm.ID + "/messages"},
			} {
				t.Run(target.kind, func(t *testing.T) {
					first := uploadFile(t, server.URL+"/api/uploads", workspace.ID, "first.txt", "first")
					second := uploadFile(t, server.URL+"/api/uploads", workspace.ID, "second.txt", "second")
					payload := map[string]string{"body": "complete replay", "nonce": target.kind, "upload_id": first.ID}
					type response struct {
						Message store.Message `json:"message"`
						Event   store.Event   `json:"event"`
					}
					created := postJSON[response](t, server.URL+target.path, payload)
					postJSON[struct{}](t, server.URL+"/api/messages/"+created.Message.ID+"/attachments", map[string]string{"upload_id": second.ID})
					replayed, status := postJSONWithStatus[response](t, server.URL+target.path, payload)
					if status != http.StatusOK || replayed.Message.ID != created.Message.ID || replayed.Event.ID != "" {
						t.Fatalf("unexpected replay: status=%d response=%#v", status, replayed)
					}
					attachments := replayed.Message.Attachments
					if len(attachments) != 2 || attachments[0].ID != first.ID || attachments[1].ID != second.ID {
						t.Fatalf("replay lost attachments: %#v", attachments)
					}
				})
			}
		})
	}
}
