package store

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	DefaultSearchPageLimit = 50
	MaxSearchPageLimit     = 100
	MaxSearchQueryRunes    = 500

	searchCursorVersion = 1
)

var ErrInvalidSearch = errors.New("invalid search request")

type SearchSort string

const (
	SearchSortRelevance SearchSort = "relevance"
	SearchSortNewest    SearchSort = "newest"
)

type SearchPageRequest struct {
	WorkspaceID          string
	ChannelID            string
	DirectConversationID string
	UserID               string
	Query                string
	Sort                 SearchSort
	Limit                int
	Cursor               string
}

type SearchCursor struct {
	Version     int        `json:"v"`
	Fingerprint string     `json:"f"`
	Sort        SearchSort `json:"s"`
	Rank        float64    `json:"r,omitempty"`
	CreatedAt   string     `json:"t"`
	MessageID   string     `json:"m"`
}

type SearchPageEntry struct {
	Hit  SearchHit
	Rank float64
}

func NormalizeSearchPageRequest(req SearchPageRequest) (SearchPageRequest, error) {
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.DirectConversationID = strings.TrimSpace(req.DirectConversationID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Query = strings.TrimSpace(req.Query)
	req.Sort = SearchSort(strings.TrimSpace(string(req.Sort)))
	req.Cursor = strings.TrimSpace(req.Cursor)

	if req.WorkspaceID == "" {
		return req, fmt.Errorf("%w: workspace_id is required", ErrInvalidSearch)
	}
	if req.UserID == "" {
		return req, fmt.Errorf("%w: user id is required", ErrInvalidSearch)
	}
	if req.ChannelID != "" && req.DirectConversationID != "" {
		return req, fmt.Errorf("%w: channel_id and direct_conversation_id are mutually exclusive", ErrInvalidSearch)
	}
	if utf8.RuneCountInString(req.Query) > MaxSearchQueryRunes {
		return req, fmt.Errorf("%w: query exceeds %d characters", ErrInvalidSearch, MaxSearchQueryRunes)
	}
	if req.Sort == "" {
		req.Sort = SearchSortRelevance
	}
	if req.Sort != SearchSortRelevance && req.Sort != SearchSortNewest {
		return req, fmt.Errorf("%w: sort must be relevance or newest", ErrInvalidSearch)
	}
	if req.Limit == 0 {
		req.Limit = DefaultSearchPageLimit
	}
	if req.Limit < 0 {
		return req, fmt.Errorf("%w: limit must be positive", ErrInvalidSearch)
	}
	if req.Limit > MaxSearchPageLimit {
		req.Limit = MaxSearchPageLimit
	}
	return req, nil
}

func CompileSQLiteSearchQuery(workspaceID, query string) string {
	terms := strings.Fields(query)
	compiled := make([]string, 0, len(terms))
	for _, term := range terms {
		compiled = append(compiled, quoteSQLiteFTSTerm(term))
	}
	if len(compiled) == 0 {
		return ""
	}
	return "workspace_id : " + quoteSQLiteFTSTerm(workspaceID) +
		" AND body : (" + strings.Join(compiled, " AND ") + ")"
}

func quoteSQLiteFTSTerm(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func DecodeSearchCursor(value string, req SearchPageRequest) (SearchCursor, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return SearchCursor{}, false, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return SearchCursor{}, false, fmt.Errorf("%w: malformed cursor", ErrInvalidSearch)
	}
	var cursor SearchCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return SearchCursor{}, false, fmt.Errorf("%w: malformed cursor", ErrInvalidSearch)
	}
	if cursor.Version != searchCursorVersion ||
		cursor.Fingerprint != searchRequestFingerprint(req) ||
		cursor.Sort != req.Sort ||
		cursor.CreatedAt == "" ||
		cursor.MessageID == "" ||
		(cursor.Sort == SearchSortRelevance && (math.IsNaN(cursor.Rank) || math.IsInf(cursor.Rank, 0))) {
		return SearchCursor{}, false, fmt.Errorf("%w: stale or mismatched cursor", ErrInvalidSearch)
	}
	return cursor, true, nil
}

func EncodeSearchCursor(req SearchPageRequest, rank float64, createdAt, messageID string) (string, error) {
	if createdAt == "" || messageID == "" || math.IsNaN(rank) || math.IsInf(rank, 0) {
		return "", fmt.Errorf("%w: invalid cursor position", ErrInvalidSearch)
	}
	cursor := SearchCursor{
		Version:     searchCursorVersion,
		Fingerprint: searchRequestFingerprint(req),
		Sort:        req.Sort,
		Rank:        rank,
		CreatedAt:   createdAt,
		MessageID:   messageID,
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func BuildSearchPage(req SearchPageRequest, entries []SearchPageEntry) (SearchPage, error) {
	page := SearchPage{
		Results: make([]SearchHit, 0, min(len(entries), req.Limit)),
	}
	hasMore := len(entries) > req.Limit
	if hasMore {
		entries = entries[:req.Limit]
	}
	for _, entry := range entries {
		page.Results = append(page.Results, entry.Hit)
	}
	if hasMore && len(entries) > 0 {
		last := entries[len(entries)-1]
		cursor, err := EncodeSearchCursor(req, last.Rank, last.Hit.CreatedAt, last.Hit.ID)
		if err != nil {
			return SearchPage{}, err
		}
		page.NextCursor = &cursor
	}
	return page, nil
}

func searchRequestFingerprint(req SearchPageRequest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.WorkspaceID,
		req.ChannelID,
		req.DirectConversationID,
		req.UserID,
		req.Query,
		string(req.Sort),
	}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
