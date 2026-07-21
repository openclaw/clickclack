package store

import "testing"

func TestResolveAvatarURL(t *testing.T) {
	t.Parallel()

	const expected = "https://gravatar.com/avatar/84059b07d4be67b806386c0aad8070a23f18836bbaae342275dc0a83414c32ee?d=identicon"
	tests := []struct {
		name      string
		avatarURL string
		email     string
		want      string
	}{
		{name: "explicit avatar wins", avatarURL: " https://example.com/avatar.png ", email: "user@example.com", want: "https://example.com/avatar.png"},
		{name: "normalized email", email: " MyEmailAddress@example.com ", want: expected},
		{name: "lowercase email", email: "myemailaddress@example.com", want: expected},
		{name: "missing email", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveAvatarURL(tt.avatarURL, tt.email); got != tt.want {
				t.Fatalf("ResolveAvatarURL(%q, %q) = %q, want %q", tt.avatarURL, tt.email, got, tt.want)
			}
		})
	}
}
