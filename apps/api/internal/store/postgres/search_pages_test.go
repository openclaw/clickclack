package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSearchMessagePagePostgresParity(t *testing.T) {
	ctx := context.Background()
	st := newIsolatedPostgresTestStore(t)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := st.EnsureBootstrap(ctx, "Search Owner", "postgres-search-owner@example.com")
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

	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: channel.ID, AuthorID: owner.ID, Body: "needle postgres root"})
	if err != nil {
		t.Fatal(err)
	}
	literalMarkers := "\ufdd0literal\ufdd1"
	literalMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      literalMarkers + " needle postgres context",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: root.ID,
		AuthorID:      owner.ID,
		Body:          "needle postgres reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, createdAt := range map[string]string{
		root.ID:           "2026-07-15T10:00:01Z",
		literalMessage.ID: "2026-07-15T10:00:02Z",
		reply.ID:          "2026-07-15T10:00:03Z",
	} {
		if _, err := st.db.ExecContext(ctx, `UPDATE messages SET created_at = $1 WHERE id = $2`, createdAt, id); err != nil {
			t.Fatal(err)
		}
	}

	request := store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Query:       "needle postgres",
		Sort:        store.SearchSortNewest,
		Limit:       2,
	}
	first, err := st.SearchMessagePage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Results) != 2 || first.Results[0].ID != reply.ID || first.Results[1].ID != literalMessage.ID || first.NextCursor == nil {
		t.Fatalf("unexpected first page %#v", first)
	}
	if first.Results[0].ParentMessageID == nil || *first.Results[0].ParentMessageID != root.ID ||
		first.Results[0].ThreadRootID != root.ID || first.Results[0].ThreadSeq == nil {
		t.Fatalf("thread routing metadata missing from %#v", first.Results[0])
	}
	if !strings.Contains(first.Results[1].Snippet, "\ufdd1") {
		t.Fatalf("literal marker text was consumed by the parser in %#v", first.Results[1])
	}
	highlighted := make([]string, 0, len(first.Results[1].Highlights))
	snippetRunes := []rune(first.Results[1].Snippet)
	for _, highlight := range first.Results[1].Highlights {
		highlighted = append(highlighted, string(snippetRunes[highlight.Start:highlight.End]))
	}
	if strings.Join(highlighted, " ") != "needle postgres" {
		t.Fatalf("unexpected highlighted text %#v in %#v", highlighted, first.Results[1])
	}

	request.Cursor = *first.NextCursor
	second, err := st.SearchMessagePage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Results) != 1 || second.Results[0].ID != root.ID || second.NextCursor != nil {
		t.Fatalf("unexpected second page %#v", second)
	}
	if second.Results[0].ReplyCount != 1 || second.Results[0].LastReplyAt == nil {
		t.Fatalf("root thread summary missing from %#v", second.Results[0])
	}

	fractionEarly, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "fractionprobe identical",
	})
	if err != nil {
		t.Fatal(err)
	}
	fractionLate, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: channel.ID,
		AuthorID:  owner.ID,
		Body:      "fractionprobe identical",
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, createdAt := range map[string]string{
		fractionEarly.ID: "2026-07-15T10:00:05.1Z",
		fractionLate.ID:  "2026-07-15T10:00:05.12Z",
	} {
		if _, err := st.db.ExecContext(ctx, `UPDATE messages SET created_at = $1 WHERE id = $2`, createdAt, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, sort := range []store.SearchSort{store.SearchSortNewest, store.SearchSortRelevance} {
		fractionRequest := store.SearchPageRequest{
			WorkspaceID: workspace.ID,
			ChannelID:   channel.ID,
			UserID:      owner.ID,
			Query:       "fractionprobe",
			Sort:        sort,
			Limit:       1,
		}
		fractionFirst, err := st.SearchMessagePage(ctx, fractionRequest)
		if err != nil {
			t.Fatal(err)
		}
		if len(fractionFirst.Results) != 1 || fractionFirst.Results[0].ID != fractionLate.ID || fractionFirst.NextCursor == nil {
			t.Fatalf("%s search ordered fractional timestamps incorrectly: %#v", sort, fractionFirst)
		}
		fractionRequest.Cursor = *fractionFirst.NextCursor
		fractionSecond, err := st.SearchMessagePage(ctx, fractionRequest)
		if err != nil {
			t.Fatal(err)
		}
		if len(fractionSecond.Results) != 1 || fractionSecond.Results[0].ID != fractionEarly.ID || fractionSecond.NextCursor != nil {
			t.Fatalf("%s search cursor lost fractional timestamp ordering: %#v", sort, fractionSecond)
		}
	}

	relevanceRequest := store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		ChannelID:   channel.ID,
		UserID:      owner.ID,
		Query:       "tieprobe",
		Limit:       1,
	}
	relevanceIDs := make(map[string]bool, 3)
	for i := 0; i < 3; i++ {
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: channel.ID,
			AuthorID:  owner.ID,
			Body:      "tieprobe identical",
		})
		if err != nil {
			t.Fatal(err)
		}
		relevanceIDs[message.ID] = false
	}
	for {
		page, err := st.SearchMessagePage(ctx, relevanceRequest)
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range page.Results {
			seen, ok := relevanceIDs[result.ID]
			if !ok || seen {
				t.Fatalf("relevance pagination returned an unexpected or duplicate result %#v", result)
			}
			relevanceIDs[result.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		relevanceRequest.Cursor = *page.NextCursor
	}
	for id, seen := range relevanceIDs {
		if !seen {
			t.Fatalf("relevance pagination skipped %s", id)
		}
	}

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Search Member", Email: "postgres-search-member@example.com"})
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
		Body:           "needle postgres private",
	})
	if err != nil {
		t.Fatal(err)
	}
	directPage, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID:          workspace.ID,
		DirectConversationID: conversation.ID,
		UserID:               owner.ID,
		Query:                "needle postgres",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(directPage.Results) != 1 || directPage.Results[0].ID != directMessage.ID {
		t.Fatalf("unexpected direct search page %#v", directPage)
	}

	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Search Outsider", Email: "postgres-search-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		ChannelID:   channel.ID,
		UserID:      outsider.ID,
		Query:       " ",
	}); err == nil {
		t.Fatal("expected empty channel search to enforce workspace membership")
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, outsider.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID:          workspace.ID,
		DirectConversationID: conversation.ID,
		UserID:               outsider.ID,
		Query:                " ",
	}); err == nil {
		t.Fatal("expected empty direct search to enforce conversation membership")
	}

	for _, indexName := range []string{"idx_messages_direct_search_fts", "idx_messages_direct_search_scope"} {
		var count int
		if err := st.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = $1`, indexName).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected search index %s", indexName)
		}
	}
}
