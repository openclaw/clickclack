package store

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestEventCursorAfter(t *testing.T) {
	cursor := func(ms uint64, entropy byte) string {
		id := ulid.ULID{}
		if err := id.SetTime(ms); err != nil {
			t.Fatal(err)
		}
		id[15] = entropy
		return "cur_" + strings.ToLower(id.String())
	}
	for _, tc := range []struct {
		name                      string
		candidate, frontier, want string
	}{
		{"empty", cursor(100, 1), "", cursor(100, 1)},
		{"already later", cursor(101, 1), cursor(100, 255), cursor(101, 1)},
		{"equal", cursor(100, 1), cursor(100, 1), cursor(101, 1)},
		{"lower entropy", cursor(100, 1), cursor(100, 2), cursor(101, 1)},
		{"older clock higher entropy", cursor(99, 2), cursor(100, 1), cursor(100, 2)},
		{"older clock lower entropy", cursor(99, 1), cursor(100, 2), cursor(101, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EventCursorAfter(tc.candidate, tc.frontier)
			if err != nil || got != tc.want {
				t.Fatalf("got %s, %v; want %s", got, err, tc.want)
			}
			if _, err := ulid.ParseStrict(strings.TrimPrefix(got, "cur_")); err != nil {
				t.Fatal(err)
			}
		})
	}
	maximum := cursor(ulid.MaxTime(), 255)
	if _, err := EventCursorAfter(maximum, maximum); err == nil {
		t.Fatal("expected timestamp exhaustion error")
	}
}
