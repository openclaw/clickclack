package store

import (
	"strings"
	"testing"
)

func TestNormalizeManagedChannelIdentity(t *testing.T) {
	t.Parallel()
	provider, ref, err := NormalizeManagedChannelIdentity(" github ", " openclaw/clickclack#125 ")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "github" || ref != "openclaw/clickclack#125" {
		t.Fatalf("unexpected normalized identity: provider=%q ref=%q", provider, ref)
	}

	tests := []struct {
		name     string
		provider string
		ref      string
	}{
		{name: "empty provider", ref: "item"},
		{name: "uppercase provider", provider: "GitHub", ref: "item"},
		{name: "provider too long", provider: strings.Repeat("a", 65), ref: "item"},
		{name: "empty ref", provider: "github"},
		{name: "ref too long", provider: "github", ref: strings.Repeat("x", 513)},
		{name: "control character in ref", provider: "github", ref: "pull\n125"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := NormalizeManagedChannelIdentity(tt.provider, tt.ref); err == nil {
				t.Fatal("expected invalid managed channel identity")
			}
		})
	}
}
