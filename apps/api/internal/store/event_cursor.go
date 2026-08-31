package store

import (
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
)

// EventCursorAfter keeps the candidate's entropy while placing it above the
// persisted workspace frontier. Callers must serialize appends through commit.
func EventCursorAfter(candidate, frontier string) (string, error) {
	if candidate > frontier {
		return candidate, nil
	}
	id, err := ulid.ParseStrict(strings.TrimPrefix(candidate, "cur_"))
	if err != nil {
		return "", fmt.Errorf("event cursor candidate: %w", err)
	}
	previous, err := ulid.ParseStrict(strings.TrimPrefix(frontier, "cur_"))
	if err != nil {
		return "", fmt.Errorf("event cursor frontier: %w", err)
	}
	// Reuse the frontier millisecond when fresh entropy already sorts above it.
	// Otherwise advance just one millisecond, preserving cross-workspace entropy.
	if err := id.SetTime(previous.Time()); err != nil {
		return "", err
	}
	if id.Compare(previous) <= 0 {
		if err := id.SetTime(previous.Time() + 1); err != nil {
			return "", err
		}
	}
	return "cur_" + strings.ToLower(id.String()), nil
}
