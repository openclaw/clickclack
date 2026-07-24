package store

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeChannelDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    *string
		wantErr bool
	}{
		{name: "empty"},
		{name: "whitespace", input: "   "},
		{name: "trimmed unicode", input: "  Coordinate café rollout 🚀  ", want: stringPointer("Coordinate café rollout 🚀")},
		{name: "279 code points", input: strings.Repeat("é", 279), want: stringPointer(strings.Repeat("é", 279))},
		{name: "280 code points", input: strings.Repeat("é", 280), want: stringPointer(strings.Repeat("é", 280))},
		{name: "281 code points", input: strings.Repeat("é", 281), wantErr: true},
		{name: "281 code points before trimming", input: " " + strings.Repeat("é", 279) + " ", wantErr: true},
		{name: "line feed", input: "one\ntwo", wantErr: true},
		{name: "carriage return", input: "one\rtwo", wantErr: true},
		{name: "tab", input: "one\ttwo", wantErr: true},
		{name: "unicode line separator", input: "one\u2028two", wantErr: true},
		{name: "unicode paragraph separator", input: "one\u2029two", wantErr: true},
		{name: "embedded control", input: "one\u0000two", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeChannelDescription(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidChannelDescription) {
					t.Fatalf("expected ErrInvalidChannelDescription, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("expected %q, got %#v", *tc.want, got)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
