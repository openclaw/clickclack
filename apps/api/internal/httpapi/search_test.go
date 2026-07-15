package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSearchHTTPContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newEmptyHTTPStore(t)
	owner, err := st.EnsureBootstrap(ctx, "Search Owner", "http-search-owner@example.com")
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
	for i := 0; i < 5; i++ {
		if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      "httpneedle message",
		}); err != nil {
			t.Fatal(err)
		}
	}

	server := httptest.NewServer(New(st, realtime.NewHub(), Options{}).Handler())
	t.Cleanup(server.Close)
	baseSearchURL := server.URL + "/api/search?workspace_id=" + url.QueryEscape(workspace.ID) +
		"&channel_id=" + url.QueryEscape(channel.ID) +
		"&q=httpneedle&sort=newest&limit=2"

	firstRequest, err := http.NewRequest(http.MethodGet, baseSearchURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstRequest.Header.Set("X-ClickClack-User", owner.ID)
	firstResponse, err := http.DefaultClient.Do(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := io.ReadAll(firstResponse.Body)
	_ = firstResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected first search status %s: %s", firstResponse.Status, firstBody)
	}
	for _, forbidden := range []string{`"message":`, `"rank":`, `"body":`} {
		if strings.Contains(string(firstBody), forbidden) {
			t.Fatalf("search response leaked legacy field %s: %s", forbidden, firstBody)
		}
	}
	var first store.SearchPage
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Results) != 2 || first.NextCursor == nil {
		t.Fatalf("unexpected first search page %#v", first)
	}

	seen := make(map[string]bool, 5)
	for _, result := range first.Results {
		seen[result.ID] = true
	}
	cursor := first.NextCursor
	for cursor != nil {
		page := getJSONAsUser[store.SearchPage](t, owner.ID, baseSearchURL+"&cursor="+url.QueryEscape(*cursor))
		if len(page.Results) > 2 {
			t.Fatalf("search page exceeded limit: %#v", page)
		}
		for _, result := range page.Results {
			if seen[result.ID] {
				t.Fatalf("search pagination repeated %s", result.ID)
			}
			seen[result.ID] = true
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("search pagination lost results: %#v", seen)
	}

	validationBaseURL := server.URL + "/api/search?workspace_id=" + url.QueryEscape(workspace.ID) +
		"&channel_id=" + url.QueryEscape(channel.ID)
	for _, invalid := range []string{
		"&q=httpneedle&limit=bad",
		"&q=httpneedle&sort=oldest",
		"&q=httpneedle&direct_conversation_id=dm_conflict",
		"&q=httpneedle&cursor=not-base64!",
		"&q=" + strings.Repeat("x", store.MaxSearchQueryRunes+1),
	} {
		expectStatusAsUser(t, owner.ID, http.MethodGet, validationBaseURL+invalid, nil, http.StatusBadRequest)
	}

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Search Member", Email: "http-search-member@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, member.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	conversation, err := st.CreateDirectConversation(ctx, store.CreateDirectConversationInput{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		MemberIDs:   []string{member.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	directMessage, _, err := st.CreateDirectMessage(ctx, store.CreateDirectMessageInput{
		ConversationID: conversation.ID,
		AuthorID:       member.ID,
		Body:           "private httpneedle",
	})
	if err != nil {
		t.Fatal(err)
	}
	directPage := getJSONAsUser[store.SearchPage](
		t,
		owner.ID,
		server.URL+"/api/search?workspace_id="+url.QueryEscape(workspace.ID)+
			"&direct_conversation_id="+url.QueryEscape(conversation.ID)+
			"&q=private",
	)
	if len(directPage.Results) != 1 || directPage.Results[0].ID != directMessage.ID {
		t.Fatalf("unexpected direct search page %#v", directPage)
	}
	workspacePage := getJSONAsUser[store.SearchPage](
		t,
		owner.ID,
		server.URL+"/api/search?workspace_id="+url.QueryEscape(workspace.ID)+"&q=private",
	)
	if len(workspacePage.Results) != 0 {
		t.Fatalf("workspace channel search leaked direct messages %#v", workspacePage)
	}
}
