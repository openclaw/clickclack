package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestQuestionHTTPLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "question-http-owner@example.com")
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
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "question-http-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	_, botToken, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Asker", Scopes: []string{"bot:write"}, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, otherToken, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Other", Scopes: []string{"bot:write"}, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
	t.Cleanup(server.Close)

	question := map[string]any{
		"external_id":        "ask_http",
		"expires_at":         time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		"responder_user_ids": []string{member.ID},
		"items": []map[string]any{
			{"id": "ship_date", "header": "Fecha", "prompt": "¿Qué día?", "options": []map[string]string{{"label": "Lun 15 sep"}, {"label": "Mar 16 sep"}}},
		},
	}
	messagesURL := server.URL + "/api/channels/" + channels[0].ID + "/messages"
	expectStatusAsUser(t, owner.ID, http.MethodPost, messagesURL, strings.NewReader(`{"body":"human question","question":{"expires_at":"2099-01-01T00:00:00Z","items":[]}}`), http.StatusForbidden)
	invalid, status := requestJSONWithBearerStatus[map[string]any](t, botToken.Token, http.MethodPost, messagesURL, map[string]any{"body": "empty", "question": map[string]any{"expires_at": question["expires_at"], "items": []any{}}})
	if status != http.StatusBadRequest || !strings.Contains(invalid["error"].(string), "invalid question") {
		t.Fatalf("invalid question status=%d body=%#v", status, invalid)
	}

	created, status, headers := requestJSONWithBearerHeaders[struct {
		Message store.Message `json:"message"`
	}](t, botToken.Token, http.MethodPost, messagesURL, map[string]any{"body": "Agent needs input: ¿Qué día?", "question": question})
	if status != http.StatusCreated || headers.Get(store.QuestionCapabilityHeader) != store.QuestionCapabilityHeaderValue {
		t.Fatalf("create status=%d capability=%q", status, headers.Get(store.QuestionCapabilityHeader))
	}
	if created.Message.Question == nil || created.Message.Question.Status != store.QuestionStatusOpen {
		t.Fatalf("created message question = %#v", created.Message.Question)
	}
	answersURL := server.URL + "/api/messages/" + created.Message.ID + "/question/answers"
	resolutionURL := server.URL + "/api/messages/" + created.Message.ID + "/question/resolution"

	botConn := dialRealtimeWithBotToken(t, server.URL, workspace.ID, botToken.Token)
	t.Cleanup(func() { _ = botConn.Close(websocket.StatusNormalClosure, "done") })
	readEventType(t, botConn, "message.created")

	answer := `{"answers":{"ship_date":["Mar 16 sep"]},"nonce":"http-answer"}`
	expectStatusWithBearer(t, botToken.Token, http.MethodPost, answersURL, strings.NewReader(answer), http.StatusForbidden)
	expectStatusAsUser(t, owner.ID, http.MethodPost, answersURL, strings.NewReader(answer), http.StatusForbidden)
	expectStatusAsUser(t, member.ID, http.MethodPost, answersURL, strings.NewReader(`{"answers":{"ship_date":["Mar 16 sep"]},"skip":true}`), http.StatusBadRequest)
	expectStatusAsUser(t, member.ID, http.MethodPost, answersURL, strings.NewReader(`{"answers":{"ship_date":["Mar 16 sep"]},"nonce":"stale-view","expected_version":9}`), http.StatusConflict)
	answered := postJSONAsUser[struct {
		Message store.Message `json:"message"`
		Events  []store.Event `json:"events"`
	}](t, member.ID, answersURL, map[string]any{"answers": map[string][]string{"ship_date": {"Mar 16 sep"}}, "nonce": "http-answer"})
	if answered.Message.Question.Status != store.QuestionStatusSubmitted || len(answered.Events) != 2 {
		t.Fatalf("answer response = %#v", answered)
	}
	submitted := readEventType(t, botConn, "question.submitted")
	if payload := submitted.Payload.(map[string]any); payload["message_id"] != created.Message.ID || payload["responder_id"] != member.ID || payload["answers"] != nil {
		t.Fatalf("question.submitted payload = %#v", submitted.Payload)
	}
	expectStatusAsUser(t, member.ID, http.MethodPost, answersURL, strings.NewReader(`{"answers":{"ship_date":["Lun 15 sep"]},"nonce":"late"}`), http.StatusConflict)

	expectStatusWithBearer(t, otherToken.Token, http.MethodPost, resolutionURL, strings.NewReader(`{"status":"answered"}`), http.StatusForbidden)
	expectStatusAsUser(t, owner.ID, http.MethodPost, resolutionURL, strings.NewReader(`{"status":"answered"}`), http.StatusForbidden)
	expectStatusWithBearer(t, botToken.Token, http.MethodPost, resolutionURL, strings.NewReader(`{"status":"answered","expected_version":1}`), http.StatusConflict)
	resolved, status := requestJSONWithBearerStatus[struct {
		Message store.Message `json:"message"`
		Event   *store.Event  `json:"event"`
	}](t, botToken.Token, http.MethodPost, resolutionURL, map[string]any{"status": "answered", "expected_version": 2})
	if status != http.StatusOK || resolved.Message.Question.Status != store.QuestionStatusAnswered || resolved.Event == nil {
		t.Fatalf("resolution status=%d body=%#v", status, resolved)
	}
	repeated, status := requestJSONWithBearerStatus[struct {
		Event *store.Event `json:"event"`
	}](t, botToken.Token, http.MethodPost, resolutionURL, map[string]any{"status": "answered"})
	if status != http.StatusOK || repeated.Event != nil {
		t.Fatalf("repeated resolution status=%d body=%#v", status, repeated)
	}

	pending, status := requestJSONWithBearerStatus[struct {
		Message store.Message `json:"message"`
	}](t, botToken.Token, http.MethodPost, messagesURL, map[string]any{"body": "still waiting", "question": question})
	if status != http.StatusCreated {
		t.Fatalf("second question status=%d", status)
	}
	latest, status := requestJSONWithBearerStatus[struct {
		Message store.Message `json:"message"`
	}](t, botToken.Token, http.MethodPost, messagesURL, map[string]any{"body": "one more", "question": question})
	if status != http.StatusCreated {
		t.Fatalf("third question status=%d", status)
	}
	type questionPage struct {
		Questions  []store.BotQuestion `json:"questions"`
		NextCursor *string             `json:"next_cursor"`
	}
	listURL := server.URL + "/api/bots/self/questions"
	firstPage, status := getJSONWithBearerStatus[questionPage](t, botToken.Token, listURL+"?limit=1")
	if status != http.StatusOK || len(firstPage.Questions) != 1 || firstPage.Questions[0].MessageID != pending.Message.ID || firstPage.NextCursor == nil || *firstPage.NextCursor != pending.Message.ID {
		t.Fatalf("first unresolved page status=%d body=%#v", status, firstPage)
	}
	secondPage, status := getJSONWithBearerStatus[questionPage](t, botToken.Token, listURL+"?limit=1&after="+*firstPage.NextCursor)
	if status != http.StatusOK || len(secondPage.Questions) != 1 || secondPage.Questions[0].MessageID != latest.Message.ID || secondPage.NextCursor != nil {
		t.Fatalf("second unresolved page status=%d body=%#v", status, secondPage)
	}
	expectStatusWithBearer(t, botToken.Token, http.MethodGet, listURL+"?limit=0", nil, http.StatusBadRequest)
	expectStatusAsUser(t, owner.ID, http.MethodGet, server.URL+"/api/bots/self/questions", nil, http.StatusForbidden)
	expectStatusAsUser(t, member.ID, http.MethodPost, server.URL+"/api/messages/msg_missing/question/answers", strings.NewReader(`{"skip":true}`), http.StatusNotFound)
}

func TestBotQuestionListingKeepsDirectScopesAndPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Owner", "question-list-owner@example.com")
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
	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Member", Email: "question-list-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	bot, fullToken, err := st.CreateBot(ctx, store.CreateBotInput{WorkspaceID: workspace.ID, DisplayName: "Asker", Scopes: []string{"bot:write"}, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	readOnlyToken, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "read only", Scopes: []string{"messages:read"}, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	activityToken, err := st.CreateBotToken(ctx, store.CreateBotTokenInput{WorkspaceID: workspace.ID, BotUserID: bot.ID, Name: "activity", Scopes: []string{"bot:write", store.AgentActivityWriteScope}, CreatedBy: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{UploadDir: filepath.Join(t.TempDir(), "uploads")}).Handler())
	t.Cleanup(server.Close)

	spec := func() *store.QuestionSpec {
		return &store.QuestionSpec{
			ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
			Items:     []store.QuestionItem{{ID: "ok", Header: "OK", Prompt: "Proceed?", Options: []store.QuestionOption{{Label: "Yes"}, {Label: "No"}}}},
		}
	}
	conversation, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{WorkspaceID: workspace.ID, UserID: bot.ID, MemberIDs: []string{member.ID}})
	if err != nil {
		t.Fatal(err)
	}
	direct, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{ConversationID: conversation.ID, AuthorID: bot.ID, Body: "private question", Question: spec()})
	if err != nil {
		t.Fatal(err)
	}
	for index := range store.MaxBotQuestionPageSize {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channels[0].ID, AuthorID: bot.ID, Body: fmt.Sprintf("question %d", index), Question: spec()}); err != nil {
			t.Fatal(err)
		}
	}

	type page struct {
		Questions  []store.BotQuestion `json:"questions"`
		NextCursor *string             `json:"next_cursor"`
	}
	listURL := server.URL + "/api/bots/self/questions?limit=" + fmt.Sprint(store.MaxBotQuestionPageSize)
	first, status := getJSONWithBearerStatus[page](t, fullToken.Token, listURL)
	if status != http.StatusOK || len(first.Questions) != store.MaxBotQuestionPageSize || first.NextCursor == nil {
		t.Fatalf("full page status=%d questions=%d next=%v", status, len(first.Questions), first.NextCursor)
	}
	second, status := getJSONWithBearerStatus[page](t, fullToken.Token, listURL+"&after="+*first.NextCursor)
	if status != http.StatusOK || len(second.Questions) != 1 || second.NextCursor != nil {
		t.Fatalf("remainder status=%d questions=%d next=%v", status, len(second.Questions), second.NextCursor)
	}
	seenDirect := false
	for _, question := range append(first.Questions, second.Questions...) {
		seenDirect = seenDirect || question.MessageID == direct.ID
	}
	if !seenDirect {
		t.Fatal("a token with dms:read must reconcile direct-message questions")
	}
	restricted, status := getJSONWithBearerStatus[page](t, readOnlyToken.Token, listURL)
	if status != http.StatusOK || len(restricted.Questions) != store.MaxBotQuestionPageSize || restricted.NextCursor != nil {
		t.Fatalf("read-only status=%d questions=%d next=%v", status, len(restricted.Questions), restricted.NextCursor)
	}
	for _, question := range restricted.Questions {
		if question.DirectConversationID != "" {
			t.Fatalf("a token without dms:read received direct question %#v", question)
		}
	}
	if _, err := st.RevokeBotToken(ctx, readOnlyToken.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	expectStatusWithBearer(t, readOnlyToken.Token, http.MethodGet, listURL, nil, http.StatusUnauthorized)

	activity := fmt.Sprintf(`{"body":"running","kind":"agent_tool","turn_id":"turn_question","question":{"expires_at":%q,"items":[{"id":"ok","header":"OK","prompt":"Proceed?"}]}}`, time.Now().Add(10*time.Minute).UTC().Format(time.RFC3339))
	expectStatusWithBearer(t, activityToken.Token, http.MethodPost, server.URL+"/api/channels/"+channels[0].ID+"/messages", strings.NewReader(activity), http.StatusBadRequest)
}

func requestJSONWithBearerHeaders[T any](t *testing.T, token, method, endpoint string, body any) (T, int, http.Header) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decode response: %v", method, endpoint, err)
	}
	return out, resp.StatusCode, resp.Header
}
