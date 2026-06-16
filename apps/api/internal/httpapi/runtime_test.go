package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

// runtimeFixture is the shared bootstrap for the runtime + turn_id contract
// tests: a sqlite store, the bootstrap owner/workspace/channel, an activity bot
// (bot:write + agent_activity:write) and a plain write bot (bot:write only, no
// inheritance), and a running httptest server.
type runtimeFixture struct {
	st          store.Store
	owner       store.User
	workspace   store.Workspace
	channel     store.Channel
	activityTok string
	writeTok    string
	server      *httptest.Server
}

func newRuntimeFixture(t *testing.T) *runtimeFixture {
	t.Helper()
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

	return &runtimeFixture{
		st:          st,
		owner:       owner,
		workspace:   workspace,
		channel:     channel,
		activityTok: activityToken.Token,
		writeTok:    writeToken.Token,
		server:      server,
	}
}

// doJSONAsUser issues a dev-auth (session) request and returns the status and
// raw body.
func doJSONAsUser(t *testing.T, userID, method, url, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ClickClack-User", userID)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, payload
}

// doJSONWithBearer issues a bot-token request and returns the status and raw
// body.
func doJSONWithBearer(t *testing.T, token, method, url, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, payload
}

func decodeRuntime(t *testing.T, payload []byte) store.ChannelRuntime {
	t.Helper()
	var wrapper struct {
		Runtime store.ChannelRuntime `json:"runtime"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		t.Fatalf("decode runtime: %v (body=%s)", err, string(payload))
	}
	return wrapper.Runtime
}

// TestChannelRuntimeHTTP exercises the composer runtime controls end to end:
// the bridge PUT (bot stamp), the picker PATCH (override), and the GET the
// composer reads, plus the authorization gates on each.
func TestChannelRuntimeHTTP(t *testing.T) {
	t.Parallel()
	f := newRuntimeFixture(t)
	endpoint := f.server.URL + "/api/channels/" + f.channel.ID + "/runtime"

	// GET before any write: a member reads an empty (defaulted) record.
	status, body := doJSONAsUser(t, f.owner.ID, http.MethodGet, endpoint, "")
	if status != http.StatusOK {
		t.Fatalf("GET runtime (empty): expected 200, got %d (%s)", status, string(body))
	}
	if rec := decodeRuntime(t, body); rec.ChannelID != f.channel.ID {
		t.Fatalf("GET runtime (empty): channel_id = %q, want %q", rec.ChannelID, f.channel.ID)
	}

	// PUT (bridge stamp) requires a bot token carrying agent_activity:write.
	snapshot := `{"default_model":"opus","default_thinking":"adaptive","model":"opus","thinking":"high","context_used":1200,"context_limit":200000}`

	// Human session cannot stamp runtime facts.
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPut, endpoint, snapshot); status != http.StatusForbidden {
		t.Fatalf("PUT runtime as human: expected 403, got %d", status)
	}
	// bot:write WITHOUT agent_activity:write cannot stamp (no inheritance).
	if status, _ := doJSONWithBearer(t, f.writeTok, http.MethodPut, endpoint, snapshot); status != http.StatusForbidden {
		t.Fatalf("PUT runtime as bot:write: expected 403, got %d", status)
	}
	// Scoped bot stamps successfully.
	status, body = doJSONWithBearer(t, f.activityTok, http.MethodPut, endpoint, snapshot)
	if status != http.StatusOK {
		t.Fatalf("PUT runtime as activity bot: expected 200, got %d (%s)", status, string(body))
	}
	if rec := decodeRuntime(t, body); rec.Model != "opus" || rec.ContextUsed != 1200 || rec.ContextLimit != 200000 {
		t.Fatalf("PUT runtime: unexpected record %+v", rec)
	}

	// PATCH (picker override) is a session write; a member may set it. A
	// plain channel-writer path is intentionally product-approved here: the
	// operator controls the next-turn channel runtime override without needing
	// the bridge's agent_activity:write scope.
	override := `{"model":"sonnet","thinking":"low"}`
	status, body = doJSONAsUser(t, f.owner.ID, http.MethodPatch, endpoint, override)
	if status != http.StatusOK {
		t.Fatalf("PATCH runtime override: expected 200, got %d (%s)", status, string(body))
	}
	if rec := decodeRuntime(t, body); rec.OverrideModel != "sonnet" || rec.OverrideThinking != "low" {
		t.Fatalf("PATCH runtime: override not recorded: %+v", rec)
	} else if rec.Model != "opus" || rec.ContextUsed != 1200 || rec.ContextLimit != 200000 {
		t.Fatalf("PATCH runtime: override clobbered bridge/effective fields: %+v", rec)
	}

	// Cookie-authenticated browser mutations must carry the CSRF header. The
	// frontend api() helper adds it; a raw unsafe browser fetch must be rejected.
	session, err := f.st.CreateSession(context.Background(), f.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	csrfClient := f.server.Client()
	cookieEndpoint := f.server.URL + "/api/channels/" + f.channel.ID + "/runtime"
	req, err := http.NewRequest(http.MethodPatch, cookieEndpoint, strings.NewReader(override))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", f.server.URL)
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	resp, err := csrfClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("PATCH runtime cookie auth without CSRF: expected 403, got %d (%s)", resp.StatusCode, string(body))
	}
	_ = resp.Body.Close()

	req, err = http.NewRequest(http.MethodPatch, cookieEndpoint, strings.NewReader(`{"model":"sonnet","thinking":"xhigh"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", f.server.URL)
	req.Header.Set(csrfHeaderName, "1")
	req.AddCookie(&http.Cookie{Name: "cc_session", Value: session.Token})
	resp, err = csrfClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	csrfBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH runtime cookie auth with CSRF: expected 200, got %d (%s)", resp.StatusCode, string(csrfBody))
	}
	if rec := decodeRuntime(t, csrfBody); rec.OverrideThinking != "xhigh" || rec.Model != "opus" {
		t.Fatalf("PATCH runtime cookie auth with CSRF: unexpected record %+v", rec)
	}

	// Picker overrides are validated at the API boundary: an unknown thinking
	// level or an oversized model string is rejected (400) before it can reach
	// the override row the bridge later applies to a gateway session.
	if status, _ = doJSONAsUser(t, f.owner.ID, http.MethodPatch, endpoint, `{"thinking":"bogus"}`); status != http.StatusBadRequest {
		t.Fatalf("PATCH runtime invalid thinking: expected 400, got %d", status)
	}
	if status, _ = doJSONAsUser(t, f.owner.ID, http.MethodPatch, endpoint, `{"model":"`+strings.Repeat("x", maxOverrideModelLen+1)+`"}`); status != http.StatusBadRequest {
		t.Fatalf("PATCH runtime oversized model: expected 400, got %d", status)
	}

	// GET reflects both the bridge snapshot and the pending override without
	// one clobbering the other.
	status, body = doJSONAsUser(t, f.owner.ID, http.MethodGet, endpoint, "")
	if status != http.StatusOK {
		t.Fatalf("GET runtime (after writes): expected 200, got %d (%s)", status, string(body))
	}
	rec := decodeRuntime(t, body)
	if rec.Model != "opus" || rec.OverrideModel != "sonnet" || rec.OverrideThinking != "xhigh" {
		t.Fatalf("GET runtime: snapshot/override merge wrong: %+v", rec)
	}

	// Malformed PATCH body is a 400.
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPatch, endpoint, "{not-json"); status != http.StatusBadRequest {
		t.Fatalf("PATCH runtime malformed: expected 400, got %d", status)
	}
	// Malformed PUT body (with a properly scoped bot) is a 400.
	if status, _ := doJSONWithBearer(t, f.activityTok, http.MethodPut, endpoint, "{not-json"); status != http.StatusBadRequest {
		t.Fatalf("PUT runtime malformed: expected 400, got %d", status)
	}
}

// TestChannelRuntimeUnknownChannel proves the GET/PATCH paths enforce channel
// access: an unknown channel id is a 404, not an empty 200.
func TestChannelRuntimeUnknownChannel(t *testing.T) {
	t.Parallel()
	f := newRuntimeFixture(t)
	endpoint := f.server.URL + "/api/channels/does-not-exist/runtime"

	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodGet, endpoint, ""); status != http.StatusNotFound {
		t.Fatalf("GET runtime unknown channel: expected 404, got %d", status)
	}
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPatch, endpoint, `{"model":"x"}`); status != http.StatusNotFound {
		t.Fatalf("PATCH runtime unknown channel: expected 404, got %d", status)
	}
}

// TestMessageTurnIDContract is the regression guard for the review finding: an
// ordinary ('message') row must not carry a turn_id (turn_id correlates agent
// activity rows only), while an activity-kind row may. It checks both the
// channel create path and the direct-message create path, which share the
// resolveMessageKind choke point.
func TestMessageTurnIDContract(t *testing.T) {
	t.Parallel()
	f := newRuntimeFixture(t)
	ctx := context.Background()

	channelEndpoint := f.server.URL + "/api/channels/" + f.channel.ID + "/messages"

	// Ordinary message with a turn_id is rejected (400), default kind. The 400
	// body must carry the sentinel text so the client sees why it failed rather
	// than an opaque error.
	if status, body := doJSONAsUser(t, f.owner.ID, http.MethodPost, channelEndpoint, `{"body":"hi","turn_id":"t1"}`); status != http.StatusBadRequest {
		t.Fatalf("channel message turn_id (default kind): expected 400, got %d (%s)", status, string(body))
	} else if !strings.Contains(string(body), store.ErrTurnIDNotAllowed.Error()) {
		t.Fatalf("channel message turn_id (default kind): 400 body missing sentinel text, got %s", string(body))
	}
	// Explicit kind="message" with a turn_id is also rejected (400).
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPost, channelEndpoint, `{"body":"hi","kind":"message","turn_id":"t1"}`); status != http.StatusBadRequest {
		t.Fatalf("channel message turn_id (kind=message): expected 400, got %d", status)
	}
	// Ordinary message without a turn_id is accepted.
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPost, channelEndpoint, `{"body":"hi"}`); status != http.StatusCreated {
		t.Fatalf("channel ordinary message: expected 201, got %d", status)
	}
	// Activity-kind row carrying a turn_id is accepted for a scoped bot.
	if status, _ := doJSONWithBearer(t, f.activityTok, http.MethodPost, channelEndpoint, `{"body":"thinking","kind":"agent_commentary","turn_id":"t1"}`); status != http.StatusCreated {
		t.Fatalf("channel activity message turn_id: expected 201, got %d", status)
	}

	// Direct-message path shares the same contract. A DM needs two members.
	other, err := f.st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Other", Email: "other-dm@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddWorkspaceMember(ctx, f.workspace.ID, other.ID, "member"); err != nil {
		t.Fatal(err)
	}
	dm, err := f.st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: f.workspace.ID,
		UserID:      f.owner.ID,
		MemberIDs:   []string{other.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	dmEndpoint := f.server.URL + "/api/dms/" + dm.ID + "/messages"

	if status, body := doJSONAsUser(t, f.owner.ID, http.MethodPost, dmEndpoint, `{"body":"hi","turn_id":"t1"}`); status != http.StatusBadRequest {
		t.Fatalf("dm message turn_id: expected 400, got %d (%s)", status, string(body))
	} else if !strings.Contains(string(body), store.ErrTurnIDNotAllowed.Error()) {
		t.Fatalf("dm message turn_id: 400 body missing sentinel text, got %s", string(body))
	}
	if status, _ := doJSONAsUser(t, f.owner.ID, http.MethodPost, dmEndpoint, `{"body":"hi"}`); status != http.StatusCreated {
		t.Fatalf("dm ordinary message: expected 201, got %d", status)
	}
}
