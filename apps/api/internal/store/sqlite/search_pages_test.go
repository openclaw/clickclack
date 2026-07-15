package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestSearchMessagePageScopesPaginationAndRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)

	owner, err := st.EnsureBootstrap(ctx, "Search Owner", "search-owner@example.com")
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
	general := channels[0]
	other, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: workspace.ID,
		Name:        "Search Other",
		UserID:      owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	root, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: general.ID, AuthorID: owner.ID, Body: "needle root"})
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{ChannelID: other.ID, AuthorID: owner.ID, Body: "needle other"})
	if err != nil {
		t.Fatal(err)
	}
	literalMarkers := "\ufdd0literal\ufdd1"
	literalMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: general.ID,
		AuthorID:  owner.ID,
		Body:      literalMarkers + " needle context",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := st.CreateThreadReply(ctx, store.CreateThreadReplyInput{
		RootMessageID: root.ID,
		AuthorID:      owner.ID,
		Body:          "needle reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, createdAt := range map[string]string{
		root.ID:           "2026-07-15T10:00:01Z",
		otherMessage.ID:   "2026-07-15T10:00:02Z",
		literalMessage.ID: "2026-07-15T10:00:03Z",
		reply.ID:          "2026-07-15T10:00:04Z",
	} {
		if _, err := st.db.ExecContext(ctx, `UPDATE messages SET created_at = ? WHERE id = ?`, createdAt, id); err != nil {
			t.Fatal(err)
		}
	}

	request := store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Query:       "needle",
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
	if !strings.Contains(first.Results[1].Snippet, literalMarkers) || len(first.Results[1].Highlights) != 1 {
		t.Fatalf("literal marker text was not preserved in %#v", first.Results[1])
	}

	request.Cursor = *first.NextCursor
	second, err := st.SearchMessagePage(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Results) != 2 || second.Results[0].ID != otherMessage.ID || second.Results[1].ID != root.ID || second.NextCursor != nil {
		t.Fatalf("unexpected second page %#v", second)
	}
	if second.Results[1].ReplyCount != 1 || second.Results[1].LastReplyAt == nil {
		t.Fatalf("root thread summary missing from %#v", second.Results[1])
	}

	relevanceRequest := store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		ChannelID:   general.ID,
		UserID:      owner.ID,
		Query:       "tieprobe",
		Limit:       1,
	}
	relevanceIDs := make(map[string]bool, 3)
	for i := 0; i < 3; i++ {
		message, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
			ChannelID: general.ID,
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

	channelPage, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		ChannelID:   other.ID,
		UserID:      owner.ID,
		Query:       "needle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(channelPage.Results) != 1 || channelPage.Results[0].ID != otherMessage.ID || channelPage.Results[0].ChannelName != other.Name {
		t.Fatalf("unexpected channel-scoped results %#v", channelPage)
	}

	member, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Search Member", Email: "search-member@example.com"})
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
		Body:           "needle private",
	})
	if err != nil {
		t.Fatal(err)
	}
	directPage, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID:          workspace.ID,
		DirectConversationID: conversation.ID,
		UserID:               owner.ID,
		Query:                "needle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(directPage.Results) != 1 || directPage.Results[0].ID != directMessage.ID || directPage.Results[0].DirectConversationID != conversation.ID {
		t.Fatalf("unexpected direct results %#v", directPage)
	}

	workspacePage, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: workspace.ID,
		UserID:      owner.ID,
		Query:       "needle",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range workspacePage.Results {
		if result.ID == directMessage.ID {
			t.Fatalf("workspace channel search leaked direct message %#v", result)
		}
	}

	outsider, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Search Outsider", Email: "search-outsider@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddWorkspaceMember(ctx, workspace.ID, outsider.ID, store.WorkspaceRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID:          workspace.ID,
		DirectConversationID: conversation.ID,
		UserID:               outsider.ID,
		Query:                "needle",
	}); err == nil || errors.Is(err, store.ErrInvalidSearch) {
		t.Fatalf("expected direct membership rejection, got %v", err)
	}
}

func TestSearchWorkspaceFTSMigrationPreservesAndScopesRows(t *testing.T) {
	ctx := context.Background()
	st, err := Open("sqlite://" + filepath.Join(t.TempDir(), "search-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	applySQLiteMigrationsBefore(t, ctx, st, "0033_search_workspace_fts.sql")

	owner, err := st.EnsureBootstrap(ctx, "Migration Owner", "search-migration@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := st.ListWorkspaces(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstWorkspace := workspaces[0]
	firstChannels, err := st.ListChannels(ctx, firstWorkspace.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstMessage, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: firstChannels[0].ID,
		AuthorID:  owner.ID,
		Body:      "migrationneedle preserved",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondWorkspace, err := st.CreateWorkspace(ctx, store.CreateWorkspaceInput{
		Name: "Search Migration Other",
		Slug: "search-migration-other",
	}, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondChannel, _, err := st.CreateChannel(ctx, store.CreateChannelInput{
		WorkspaceID: secondWorkspace.ID,
		Name:        "Search Migration",
		UserID:      owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateMessage(ctx, store.CreateMessageInput{
		ChannelID: secondChannel.ID,
		AuthorID:  owner.ID,
		Body:      "migrationneedle separate",
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	compiled := store.CompileSQLiteSearchQuery(firstWorkspace.ID, "migrationneedle")
	var indexedMatches int
	if err := st.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM messages_fts
		WHERE messages_fts MATCH ?`, compiled).Scan(&indexedMatches); err != nil {
		t.Fatal(err)
	}
	if indexedMatches != 1 {
		t.Fatalf("expected workspace filter inside FTS index, got %d matches", indexedMatches)
	}
	page, err := st.SearchMessagePage(ctx, store.SearchPageRequest{
		WorkspaceID: firstWorkspace.ID,
		UserID:      owner.ID,
		Query:       "migrationneedle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].ID != firstMessage.ID {
		t.Fatalf("migration did not preserve searchable rows: %#v", page)
	}
}

func TestSearchMessagePageUsesFTSIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newTestStore(t)
	plan := explainQueryPlan(t, ctx, st, `
		EXPLAIN QUERY PLAN
		SELECT m.id
		FROM messages_fts
		JOIN messages m ON m.id = messages_fts.message_id
		WHERE messages_fts.workspace_id = ?
		  AND messages_fts MATCH ?
		  AND m.channel_id = ?
		  AND m.direct_conversation_id IS NULL
		  AND m.deleted_at IS NULL
		  AND m.kind = 'message'
		ORDER BY bm25(messages_fts), m.created_at DESC, m.id DESC
		LIMIT ?`,
		"wsp_search", `"needle"`, "chn_search", store.DefaultSearchPageLimit+1,
	)
	if !strings.Contains(plan, "SCAN messages_fts VIRTUAL TABLE INDEX") {
		t.Fatalf("expected FTS5 virtual table search, got:\n%s", plan)
	}
	for line := range strings.SplitSeq(plan, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "SCAN m ") {
			t.Fatalf("search should not scan the messages table, got:\n%s", plan)
		}
	}
}
