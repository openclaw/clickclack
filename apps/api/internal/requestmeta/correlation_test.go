package requestmeta

import (
	"context"
	"strings"
	"testing"
)

func TestCorrelationIDContextAcceptsOnlyBoundedSafeValues(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("a", 128)
	if got := CorrelationID(WithCorrelationID(context.Background(), valid)); got != valid {
		t.Fatalf("correlation ID = %q, want %q", got, valid)
	}
	for _, invalid := range []string{"", "unsafe correlation", strings.Repeat("a", 129)} {
		if ValidCorrelationID(invalid) {
			t.Fatalf("expected invalid correlation ID %q", invalid)
		}
		if got := CorrelationID(WithCorrelationID(context.Background(), invalid)); got != "" {
			t.Fatalf("invalid correlation ID persisted as %q", got)
		}
	}
}
