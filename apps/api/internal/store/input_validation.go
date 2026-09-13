package store

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,31}$`)

func NormalizeClientNonce(value string) (string, error) {
	nonce := strings.TrimSpace(value)
	if !utf8.ValidString(nonce) {
		return "", errors.New("nonce must be valid UTF-8")
	}
	if strings.IndexByte(nonce, 0) >= 0 {
		return "", errors.New("nonce must not contain NUL")
	}
	if utf8.RuneCountInString(nonce) > 128 {
		return "", errors.New("nonce is too long")
	}
	return nonce, nil
}

func NormalizeHandle(value string) (string, error) {
	handle := strings.ToLower(strings.TrimSpace(value))
	handle = strings.TrimPrefix(handle, "@")
	if handle == "" {
		return "", nil
	}
	if !handlePattern.MatchString(handle) {
		return "", errors.New("handle must be 2-32 chars using letters, numbers, underscores, or dashes")
	}
	return handle, nil
}

func NormalizeAvatarURL(value string) (string, error) {
	avatarURL := strings.TrimSpace(value)
	if avatarURL == "" {
		return "", nil
	}
	if len(avatarURL) > 500 {
		return "", errors.New("avatar_url is too long")
	}
	parsed, err := url.Parse(avatarURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("avatar_url must be an http or https URL")
	}
	return avatarURL, nil
}

func NormalizeUserProfilePatch(displayNameInput, handleInput, avatarURLInput *string) (*string, *string, *string, error) {
	var displayName *string
	if displayNameInput != nil {
		normalized := strings.TrimSpace(*displayNameInput)
		if normalized == "" {
			return nil, nil, nil, errors.New("display_name is required")
		}
		if len(normalized) > 80 {
			return nil, nil, nil, errors.New("display_name is too long")
		}
		displayName = &normalized
	}
	var handle *string
	if handleInput != nil {
		normalized, err := NormalizeHandle(*handleInput)
		if err != nil {
			return nil, nil, nil, err
		}
		handle = &normalized
	}
	var avatarURL *string
	if avatarURLInput != nil {
		normalized, err := NormalizeAvatarURL(*avatarURLInput)
		if err != nil {
			return nil, nil, nil, err
		}
		avatarURL = &normalized
	}
	return displayName, handle, avatarURL, nil
}

func NormalizeUserProfile(displayNameInput, handleInput, avatarURLInput string) (string, string, string, error) {
	displayName := strings.TrimSpace(displayNameInput)
	if displayName == "" {
		return "", "", "", errors.New("display_name is required")
	}
	if len(displayName) > 80 {
		return "", "", "", errors.New("display_name is too long")
	}
	handle, err := NormalizeHandle(handleInput)
	if err != nil {
		return "", "", "", err
	}
	avatarURL, err := NormalizeAvatarURL(avatarURLInput)
	if err != nil {
		return "", "", "", err
	}
	return displayName, handle, avatarURL, nil
}
