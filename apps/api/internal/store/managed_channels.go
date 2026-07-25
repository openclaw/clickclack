package store

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ManagedChannelActionCreated   = "created"
	ManagedChannelActionUpdated   = "updated"
	ManagedChannelActionUnchanged = "unchanged"

	maxExternalRefRunes = 512
)

var (
	externalProviderPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

	ErrManagedChannelIdentityImmutable = errors.New("reconciled managed channel identity is immutable")
)

type ReconcileManagedChannelInput struct {
	WorkspaceID      string
	UserID           string
	ExternalProvider string
	ExternalRef      string
	Name             string
	Kind             string
	Archived         bool
	ExternalURL      string
	SidebarSection   string
}

type ReconcileManagedChannelResult struct {
	Channel Channel `json:"channel"`
	Action  string  `json:"action"`
	Event   *Event  `json:"event,omitempty"`
}

func NormalizeManagedChannelIdentity(provider, ref string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	ref = strings.TrimSpace(ref)
	if !externalProviderPattern.MatchString(provider) {
		return "", "", errors.New("external_provider must be 1-64 lowercase letters, digits, dots, underscores, or hyphens")
	}
	if ref == "" {
		return "", "", errors.New("external_ref is required")
	}
	if !utf8.ValidString(ref) || utf8.RuneCountInString(ref) > maxExternalRefRunes {
		return "", "", errors.New("external_ref must be valid UTF-8 with at most 512 characters")
	}
	for _, value := range ref {
		if unicode.IsControl(value) {
			return "", "", errors.New("external_ref cannot contain control characters")
		}
	}
	return provider, ref, nil
}
