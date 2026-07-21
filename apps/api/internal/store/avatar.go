package store

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func ResolveAvatarURL(avatarURL, email string) string {
	if explicit := strings.TrimSpace(avatarURL); explicit != "" {
		return explicit
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(normalizedEmail))
	return fmt.Sprintf("https://gravatar.com/avatar/%x?d=identicon", hash)
}
