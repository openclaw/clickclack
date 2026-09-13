package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPASignInCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		options Options
		want    string
	}{
		{"unconfigured", Options{}, `[]`},
		{"openclaw_id_only", Options{OpenClawID: OpenClawIDConfig{ClientID: "test-client"}}, `[]`},
		{"openclaw_secret_only", Options{OpenClawID: OpenClawIDConfig{ClientSecret: "test-secret"}}, `[]`},
		{"openclaw", Options{OpenClawID: OpenClawIDConfig{ClientID: "test-client", ClientSecret: "test-secret"}}, `["openclaw"]`},
		{"all", Options{GitHubOAuth: GitHubOAuthConfig{ClientID: "test-client", ClientSecret: "test-secret"}, PasswordAuthEnabled: true, OpenClawID: OpenClawIDConfig{ClientID: "test-client", ClientSecret: "test-secret"}}, `["github","password","openclaw"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := New(nil, nil, tc.options)
			for _, path := range []string{"/", "/app"} {
				response := httptest.NewRecorder()
				server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authMethods":`+tc.want) {
					t.Fatalf("%s: expected authMethods %s in SPA runtime config, got status %d", path, tc.want, response.Code)
				}
			}
		})
	}
}
