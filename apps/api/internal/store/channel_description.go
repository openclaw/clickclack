package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxChannelDescriptionRunes = 280

var ErrInvalidChannelDescription = errors.New("invalid channel description")

func NormalizeChannelDescription(value string) (*string, error) {
	for _, r := range value {
		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) {
			return nil, fmt.Errorf("%w: must be a single line without control characters", ErrInvalidChannelDescription)
		}
	}
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > MaxChannelDescriptionRunes {
		return nil, fmt.Errorf("%w: must be at most %d characters", ErrInvalidChannelDescription, MaxChannelDescriptionRunes)
	}
	if trimmed == "" {
		return nil, nil
	}
	return &trimmed, nil
}
