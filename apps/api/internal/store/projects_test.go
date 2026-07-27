package store

import (
	"strings"
	"testing"
)

func TestValidateCreateProjectInput(t *testing.T) {
	t.Parallel()
	valid := CreateProjectInput{
		Name:          "ClickClack",
		WebhookSecret: "secret",
		Repositories: []CreateProjectRepositoryInput{{
			Owner:    "openclaw",
			Name:     "clickclack",
			FullName: "openclaw/clickclack",
			URL:      "https://github.com/openclaw/clickclack",
		}},
	}
	if err := ValidateCreateProjectInput(valid); err != nil {
		t.Fatalf("valid project rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*CreateProjectInput)
	}{
		{"long name", func(input *CreateProjectInput) { input.Name = strings.Repeat("a", 81) }},
		{"long description", func(input *CreateProjectInput) { input.Description = strings.Repeat("a", 501) }},
		{"noncanonical repository", func(input *CreateProjectInput) { input.Repositories[0].FullName = "OpenClaw/ClickClack" }},
		{"noncanonical URL", func(input *CreateProjectInput) { input.Repositories[0].URL = "http://github.com/openclaw/clickclack" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			input.Repositories = append([]CreateProjectRepositoryInput(nil), valid.Repositories...)
			test.mutate(&input)
			if err := ValidateCreateProjectInput(input); err == nil {
				t.Fatal("expected project input to be rejected")
			}
		})
	}
}
