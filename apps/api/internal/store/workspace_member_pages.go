package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidWorkspaceMemberPage is returned when a workspace-member listing
// request uses an invalid limit, filter, or cursor.
var ErrInvalidWorkspaceMemberPage = errors.New("invalid workspace member page request")

const (
	defaultWorkspaceMemberPageLimit = 100
	maxWorkspaceMemberPageLimit     = 200
	workspaceMemberCursorVersion    = 1
)

type WorkspaceMember struct {
	WorkspaceID string `json:"workspace_id"`
	User        User   `json:"user"`
	Role        string `json:"role"`
	JoinedAt    string `json:"joined_at"`
}

type WorkspaceMemberPageRequest struct {
	Limit  int
	Cursor string
	Query  string
	Role   string
}

type WorkspaceMemberPage struct {
	Members    []WorkspaceMember `json:"members"`
	NextCursor string            `json:"next_cursor,omitempty"`
	HasMore    bool              `json:"has_more"`
	TotalCount *int              `json:"total_count,omitempty"`
}

type WorkspaceMemberCursor struct {
	Version    int    `json:"v"`
	RoleSort   int    `json:"r"`
	SortName   string `json:"n"`
	SortHandle string `json:"h"`
	UserID     string `json:"u"`
	Query      string `json:"q,omitempty"`
	Role       string `json:"role,omitempty"`
}

func NormalizeWorkspaceMemberPageRequest(req WorkspaceMemberPageRequest) (WorkspaceMemberPageRequest, error) {
	req.Query = strings.ToLower(strings.TrimSpace(req.Query))
	req.Role = strings.TrimSpace(req.Role)
	req.Cursor = strings.TrimSpace(req.Cursor)
	if req.Limit == 0 {
		req.Limit = defaultWorkspaceMemberPageLimit
	}
	if req.Limit < 0 {
		return req, fmt.Errorf("%w: limit must be positive", ErrInvalidWorkspaceMemberPage)
	}
	if req.Limit > maxWorkspaceMemberPageLimit {
		req.Limit = maxWorkspaceMemberPageLimit
	}
	if req.Role != "" && !ValidWorkspaceMemberRole(req.Role) {
		return req, fmt.Errorf("%w: invalid role filter", ErrInvalidWorkspaceMemberPage)
	}
	return req, nil
}

func ValidWorkspaceMemberRole(role string) bool {
	switch role {
	case WorkspaceRoleOwner, WorkspaceRoleModerator, WorkspaceRoleMember, WorkspaceRoleBot, WorkspaceRoleGuest:
		return true
	default:
		return false
	}
}

func WorkspaceMemberRoleSort(role string) int {
	switch role {
	case WorkspaceRoleOwner:
		return 0
	case WorkspaceRoleModerator:
		return 1
	case WorkspaceRoleMember:
		return 2
	case WorkspaceRoleBot:
		return 3
	case WorkspaceRoleGuest:
		return 4
	default:
		return 9
	}
}

func DecodeWorkspaceMemberCursor(value string) (WorkspaceMemberCursor, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return WorkspaceMemberCursor{}, false, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return WorkspaceMemberCursor{}, false, fmt.Errorf("%w: malformed cursor", ErrInvalidWorkspaceMemberPage)
	}
	var cursor WorkspaceMemberCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return WorkspaceMemberCursor{}, false, fmt.Errorf("%w: malformed cursor", ErrInvalidWorkspaceMemberPage)
	}
	if cursor.Version != workspaceMemberCursorVersion || cursor.UserID == "" {
		return WorkspaceMemberCursor{}, false, fmt.Errorf("%w: malformed cursor", ErrInvalidWorkspaceMemberPage)
	}
	return cursor, true, nil
}

func EncodeWorkspaceMemberCursor(cursor WorkspaceMemberCursor) (string, error) {
	cursor.Version = workspaceMemberCursorVersion
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
