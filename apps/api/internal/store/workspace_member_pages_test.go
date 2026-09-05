package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

// The sqlite/postgres members boundary suites drive ListWorkspaceMemberPage on
// the happy path (valid roles, real round-tripped cursors); these pin the
// error/boundary arms that path cannot reach: limit default/clamp/reject,
// invalid role filter, and malformed/stale cursor rejection.

func TestNormalizeWorkspaceMemberPageRequest_ZeroLimitDefaults(t *testing.T) {
	out, err := NormalizeWorkspaceMemberPageRequest(WorkspaceMemberPageRequest{Limit: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Limit != defaultWorkspaceMemberPageLimit {
		t.Errorf("zero limit: got %d, want default %d", out.Limit, defaultWorkspaceMemberPageLimit)
	}
}

func TestNormalizeWorkspaceMemberPageRequest_OverMaxClamps(t *testing.T) {
	out, err := NormalizeWorkspaceMemberPageRequest(WorkspaceMemberPageRequest{Limit: maxWorkspaceMemberPageLimit + 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Limit != maxWorkspaceMemberPageLimit {
		t.Errorf("over-max limit: got %d, want clamp %d", out.Limit, maxWorkspaceMemberPageLimit)
	}
}

func TestNormalizeWorkspaceMemberPageRequest_NegativeLimitRejected(t *testing.T) {
	_, err := NormalizeWorkspaceMemberPageRequest(WorkspaceMemberPageRequest{Limit: -1})
	if !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Fatalf("negative limit: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
}

func TestNormalizeWorkspaceMemberPageRequest_InvalidRoleFilterRejected(t *testing.T) {
	if _, err := NormalizeWorkspaceMemberPageRequest(WorkspaceMemberPageRequest{Limit: 10, Role: ""}); err != nil {
		t.Errorf("empty role filter should pass, got %v", err)
	}
	_, err := NormalizeWorkspaceMemberPageRequest(WorkspaceMemberPageRequest{Limit: 10, Role: "wizard"})
	if !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Fatalf("invalid role filter: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
}

func TestDecodeWorkspaceMemberCursor_MalformedRejected(t *testing.T) {
	notBase64 := "!!!not-base64!!!"
	if _, _, err := DecodeWorkspaceMemberCursor(notBase64); !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Errorf("bad base64: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	if _, _, err := DecodeWorkspaceMemberCursor(notJSON); !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Errorf("bad json: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
}

// A cursor from a different schema version, or one missing the tie-break user id,
// must be rejected: both are required for a stable keyset page.
func TestDecodeWorkspaceMemberCursor_VersionAndUserIDRequired(t *testing.T) {
	encode := func(c WorkspaceMemberCursor) string {
		payload, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(payload)
	}
	wrongVersion := encode(WorkspaceMemberCursor{Version: workspaceMemberCursorVersion + 1, UserID: "u1"})
	if _, _, err := DecodeWorkspaceMemberCursor(wrongVersion); !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Errorf("wrong version: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
	missingUser := encode(WorkspaceMemberCursor{Version: workspaceMemberCursorVersion, UserID: ""})
	if _, _, err := DecodeWorkspaceMemberCursor(missingUser); !errors.Is(err, ErrInvalidWorkspaceMemberPage) {
		t.Errorf("missing user id: got err %v, want ErrInvalidWorkspaceMemberPage", err)
	}
}
