package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

// TestAgentActivityMessageAuthz is the authorization gate for durable agent
// activity messages (kind = agent_commentary / agent_tool). It mirrors the
// authz matrix that previously guarded the ephemeral agent.progress frame, but
// now the activity rows are durable messages on the normal create path:
//
//   - a human session can never create an activity-kind message (403),
//   - an activity kind requires a BOT token carrying agent_activity:write; a
//     bot:write token MUST NOT inherit it (403, no bundle inheritance),
//   - an unknown kind is a 400,
//   - a scoped bot can create both activity kinds (success),
//   - an ordinary message is unaffected for any caller with messages:write.
func TestAgentActivityMessageAuthz(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
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
	channel := channels[0]

	// A bridge bot WITH the explicit activity scope.
	activityBot, activityToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Activity Bot",
		Scopes:      []string{"bot:write", store.AgentActivityWriteScope},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, activityBot.ID, "bot"); err != nil {
		t.Fatal(err)
	}

	// A bridge bot WITHOUT the activity scope (bot:write only). Per the
	// no-inheritance rule this must NOT gain agent_activity:write.
	writeBot, writeToken, err := st.CreateBot(ctx, store.CreateBotInput{
		WorkspaceID: workspace.ID,
		OwnerUserID: owner.ID,
		DisplayName: "Write Bot",
		Scopes:      []string{"bot:write"},
		CreatedBy:   owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, writeBot.ID, "bot"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)
	endpoint := server.URL + "/api/channels/" + channel.ID + "/messages"

	commentary := `{"body":"thinking about it","kind":"agent_commentary","turn_id":"t1"}`
	toolFrame := `{"body":"ran bash","kind":"agent_tool","turn_id":"t1"}`
	unknownKind := `{"body":"nope","kind":"agent_bogus"}`
	ordinary := `{"body":"hello channel"}`

	// 1. Human (dev-auth) session cannot create an activity-kind message.
	expectStatusAsUser(t, owner.ID, http.MethodPost, endpoint, strings.NewReader(commentary), http.StatusForbidden)

	// 2. bot:write WITHOUT agent_activity:write is rejected (no inheritance).
	expectStatusWithBearer(t, writeToken.Token, http.MethodPost, endpoint, strings.NewReader(commentary), http.StatusForbidden)

	// 3. Unknown kind is a 400, regardless of scope.
	expectStatusWithBearer(t, activityToken.Token, http.MethodPost, endpoint, strings.NewReader(unknownKind), http.StatusBadRequest)

	// 4. Scoped bot can create both activity kinds.
	expectStatusWithBearer(t, activityToken.Token, http.MethodPost, endpoint, strings.NewReader(commentary), http.StatusCreated)
	expectStatusWithBearer(t, activityToken.Token, http.MethodPost, endpoint, strings.NewReader(toolFrame), http.StatusCreated)

	// 5. Ordinary messages are unaffected for any messages:write caller.
	expectStatusAsUser(t, owner.ID, http.MethodPost, endpoint, strings.NewReader(ordinary), http.StatusCreated)
	expectStatusWithBearer(t, writeToken.Token, http.MethodPost, endpoint, strings.NewReader(ordinary), http.StatusCreated)
}

// TestAgentActivityMessagePrivacy proves a durable activity message inherits the
// same membership privacy as any channel message: a user who is not a member of
// the workspace cannot read the channel that holds it (the activity row never
// leaks to a non-member).
func TestAgentActivityMessagePrivacy(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
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
	channel := channels[0]

	// Seed a durable activity message in the channel via the store directly.
	activity, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "private commentary", Kind: store.MessageKindAgentCommentary, TurnID: "t1"})
	if err != nil {
		t.Fatal(err)
	}

	// A user who is NOT a member of the workspace.
	stranger, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Stranger", Email: "stranger@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(dataDir, "uploads")}).Handler())
	t.Cleanup(server.Close)

	// The non-member cannot list the channel's messages or fetch the activity
	// row by id: the store's membership check denies access (surfaced as a 4xx),
	// so the activity body never reaches a non-member.
	expectStatusNot2xxAsUser(t, stranger.ID, http.MethodGet, server.URL+"/api/channels/"+channel.ID+"/messages")
	expectStatusNot2xxAsUser(t, stranger.ID, http.MethodGet, server.URL+"/api/messages/"+activity.ID)
	// The owner (a member) can read it.
	expectStatusAsUser(t, owner.ID, http.MethodGet, server.URL+"/api/messages/"+activity.ID, nil, http.StatusOK)
}

// expectStatusNot2xxAsUser asserts a dev-auth request as userID is rejected with
// a non-2xx status (the exact 400-vs-403 mapping is an internal detail; the
// privacy invariant is simply that the caller is denied).
func expectStatusNot2xxAsUser(t *testing.T, userID, method, endpoint string) {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("%s %s as %s: expected a denial (>=400), got %s", method, endpoint, userID, resp.Status)
	}
}

// TestAgentActivityScopeExcludedFromBotBundles asserts the new scope is NOT part
// of any bot:* bundle, so existing deployments that minted bot:read/write/admin
// tokens gain no new capability.
func TestAgentActivityScopeExcludedFromBotBundles(t *testing.T) {
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
	owner, err := st.EnsureBootstrap(ctx, "Owner", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaces[0]
	for _, bundle := range []string{"bot:read", "bot:write", "bot:admin"} {
		_, token, err := st.CreateBot(ctx, store.CreateBotInput{
			WorkspaceID: workspace.ID,
			OwnerUserID: owner.ID,
			DisplayName: "Bundle Bot " + bundle,
			Scopes:      []string{bundle},
			CreatedBy:   owner.ID,
		})
		if err != nil {
			t.Fatalf("create bot for %s: %v", bundle, err)
		}
		for _, scope := range token.Scopes {
			if scope == store.AgentActivityWriteScope {
				t.Fatalf("%s bundle unexpectedly granted %s", bundle, store.AgentActivityWriteScope)
			}
		}
	}
}
