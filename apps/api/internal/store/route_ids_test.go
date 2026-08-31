package store

import (
	"strings"
	"testing"
)

func TestNewRouteIDFormat(t *testing.T) {
	t.Parallel()
	for _, prefix := range []byte{'T', 'C', 'D', 'M'} {
		t.Run(string(prefix), func(t *testing.T) {
			id, err := NewRouteID(prefix)
			if err != nil {
				t.Fatal(err)
			}
			if len(id) != 17 || id[0] != prefix {
				t.Fatalf("route ID %q must have prefix %q and a 16-character payload", id, prefix)
			}
			if strings.Trim(id[1:], "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" {
				t.Fatalf("route ID %q contains characters outside uppercase Crockford base32", id)
			}
		})
	}
}
