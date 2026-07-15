package store

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeSearchPageRequest(t *testing.T) {
	t.Parallel()
	req, err := NormalizeSearchPageRequest(SearchPageRequest{
		WorkspaceID: " wsp_1 ",
		UserID:      " usr_1 ",
		Query:       "  hello world  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.WorkspaceID != "wsp_1" || req.UserID != "usr_1" || req.Query != "hello world" {
		t.Fatalf("unexpected normalized request %#v", req)
	}
	if req.Sort != SearchSortRelevance || req.Limit != DefaultSearchPageLimit {
		t.Fatalf("unexpected defaults %#v", req)
	}
}

func TestNormalizeSearchPageRequestRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	base := SearchPageRequest{WorkspaceID: "wsp_1", UserID: "usr_1", Query: "hello"}
	tests := []SearchPageRequest{
		{UserID: base.UserID, Query: base.Query},
		{WorkspaceID: base.WorkspaceID, Query: base.Query},
		{WorkspaceID: base.WorkspaceID, UserID: base.UserID, Query: base.Query, ChannelID: "chn_1", DirectConversationID: "dm_1"},
		{WorkspaceID: base.WorkspaceID, UserID: base.UserID, Query: base.Query, Sort: "oldest"},
		{WorkspaceID: base.WorkspaceID, UserID: base.UserID, Query: base.Query, Limit: -1},
		{WorkspaceID: base.WorkspaceID, UserID: base.UserID, Query: strings.Repeat("x", MaxSearchQueryRunes+1)},
	}
	for _, req := range tests {
		if _, err := NormalizeSearchPageRequest(req); !errors.Is(err, ErrInvalidSearch) {
			t.Fatalf("expected invalid search error for %#v, got %v", req, err)
		}
	}
}

func TestCompileSQLiteSearchQueryQuotesTerms(t *testing.T) {
	t.Parallel()
	if got := CompileSQLiteSearchQuery(`hello OR "quoted"`); got != `"hello" AND "OR" AND """quoted"""` {
		t.Fatalf("unexpected compiled query %q", got)
	}
}

func TestSearchCursorRoundTripAndRequestBinding(t *testing.T) {
	t.Parallel()
	req, err := NormalizeSearchPageRequest(SearchPageRequest{
		WorkspaceID: "wsp_1",
		ChannelID:   "chn_1",
		UserID:      "usr_1",
		Query:       "hello",
		Sort:        SearchSortRelevance,
		Limit:       25,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeSearchCursor(req, 0.75, "2026-07-15T10:00:00Z", "msg_1")
	if err != nil {
		t.Fatal(err)
	}
	cursor, ok, err := DecodeSearchCursor(encoded, req)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || cursor.Rank != 0.75 || cursor.CreatedAt != "2026-07-15T10:00:00Z" || cursor.MessageID != "msg_1" {
		t.Fatalf("unexpected cursor %#v", cursor)
	}

	resized := req
	resized.Limit = 50
	if _, ok, err := DecodeSearchCursor(encoded, resized); err != nil || !ok {
		t.Fatalf("expected cursor to allow a different page size, ok=%v err=%v", ok, err)
	}

	other := req
	other.Query = "different"
	if _, _, err := DecodeSearchCursor(encoded, other); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("expected mismatched cursor rejection, got %v", err)
	}
}

func TestDecodeSearchCursorRejectsMalformedValue(t *testing.T) {
	t.Parallel()
	req, err := NormalizeSearchPageRequest(SearchPageRequest{
		WorkspaceID: "wsp_1",
		UserID:      "usr_1",
		Query:       "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeSearchCursor("not-base64!", req); !errors.Is(err, ErrInvalidSearch) {
		t.Fatalf("expected malformed cursor rejection, got %v", err)
	}
}
