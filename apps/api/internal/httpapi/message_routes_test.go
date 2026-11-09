package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

func TestEnsureMessageRouteEndpointIsLazyBoundedAndPermissionChecked(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Route Owner", "route-owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Route Member", Email: "route-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Route Guest", Email: "route-guest@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Route Outsider", Email: "route-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, guest.ID, store.WorkspaceRoleGuest); err != nil {
		t.Fatal(err)
	}
	channels, err := st.ListChannels(ctx, workspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channel := channels[0]
	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "stable citation"})
	if err != nil {
		t.Fatal(err)
	}
	if root.RouteID != "" {
		t.Fatalf("route was allocated before first citation request: %#v", root)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	endpoint := server.URL + "/api/messages/" + root.ID + "/route"
	first := postJSONAsUser[struct {
		Message store.Message `json:"message"`
	}](t, member.ID, endpoint, struct{}{})
	if len(first.Message.RouteID) != 17 || first.Message.RouteID[0] != 'M' {
		t.Fatalf("unexpected ensured route: %#v", first.Message)
	}
	repeated := postJSONAsUser[struct {
		Message store.Message `json:"message"`
	}](t, owner.ID, endpoint, struct{}{})
	if repeated.Message.RouteID != first.Message.RouteID {
		t.Fatalf("repeated ensure changed route: %q vs %q", first.Message.RouteID, repeated.Message.RouteID)
	}

	target := getJSONAsUser[struct {
		Route store.RouteTarget `json:"route"`
	}](t, member.ID, server.URL+"/api/routes/"+workspace.RouteID+"/"+first.Message.RouteID)
	if target.Route.TargetID != root.ID || target.Route.ParentID != channel.ID || target.Route.CanonicalPath != "/app/"+workspace.RouteID+"/"+first.Message.RouteID {
		t.Fatalf("unexpected citation target: %#v", target.Route)
	}

	dm, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmRoot, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: dm.ID, AuthorID: owner.ID, Body: "private root"})
	if err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{RootMessageID: root.ID, AuthorID: owner.ID, Body: "reply"})
	if err != nil {
		t.Fatal(err)
	}

	wantStatus, wantBody := postRouteStatus(t, outsider.ID, endpoint)
	if wantStatus != http.StatusNotFound {
		t.Fatalf("outsider ensure returned %d: %s", wantStatus, wantBody)
	}
	for name, testCase := range map[string]struct {
		userID   string
		endpoint string
	}{
		"guest-forbidden": {userID: guest.ID, endpoint: endpoint},
		"missing":         {userID: owner.ID, endpoint: server.URL + "/api/messages/msg_missing/route"},
		"dm-root":         {userID: owner.ID, endpoint: server.URL + "/api/messages/" + dmRoot.ID + "/route"},
		"reply":           {userID: owner.ID, endpoint: server.URL + "/api/messages/" + reply.ID + "/route"},
	} {
		t.Run(name, func(t *testing.T) {
			status, body := postRouteStatus(t, testCase.userID, testCase.endpoint)
			if status != wantStatus || body != wantBody {
				t.Fatalf("not-found contract diverged: got %d %q, want %d %q", status, body, wantStatus, wantBody)
			}
		})
	}

	archivedRoot, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "archived root"})
	if err != nil {
		t.Fatal(err)
	}
	patchJSON[struct {
		Channel store.Channel `json:"channel"`
	}](t, server.URL+"/api/channels/"+channel.ID, map[string]bool{"archived": true})
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/channels/"+channel.ID+"/messages", map[string]string{"body": "archived"}, http.StatusConflict)
	archived := postJSONAsUser[struct {
		Message store.Message `json:"message"`
	}](t, member.ID, server.URL+"/api/messages/"+archivedRoot.ID+"/route", struct{}{})
	if archived.Message.RouteID == "" {
		t.Fatal("archived channel root did not receive a route")
	}

	deleted, _, err := st.DeleteMessage(ctx, store.DeleteMessageInput{MessageID: root.ID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil {
		t.Fatal("expected soft-deleted root")
	}
	thread := getJSONAsUser[struct {
		Root store.Message `json:"root"`
	}](t, member.ID, server.URL+"/api/messages/"+root.ID+"/thread")
	if thread.Root.DeletedAt == nil || thread.Root.RouteID != first.Message.RouteID {
		t.Fatalf("soft-deleted citation did not resolve to its tombstone: %#v", thread.Root)
	}
	expectStatusAsUser(t, outsider.ID, http.MethodGet, server.URL+"/api/routes/"+workspace.RouteID+"/"+first.Message.RouteID, nil, http.StatusNotFound)
}

func postRouteStatus(t *testing.T, userID, endpoint string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}
