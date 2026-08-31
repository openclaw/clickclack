package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	sqlitestore "github.com/openclaw/clickclack/apps/api/internal/store/sqlite"
)

const passwordTestSecret = "correct horse battery"

// newPasswordTestServer returns a server with password login enabled, one
// account that has a password, and one that does not. Each test gets its own
// server so the per-identifier budget is never shared across tests.
func newPasswordTestServer(t *testing.T, passwordAuthEnabled bool) (*httptest.Server, store.Store, store.User, store.User) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := sqlitestore.Open("sqlite://" + filepath.Join(dataDir, "clickclack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	enrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Enrolled", Email: "enrolled@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := passwordauth.Hash(passwordTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserPassword(ctx, enrolled.ID, hash); err != nil {
		t.Fatal(err)
	}
	unenrolled, err := st.CreateUser(ctx, store.CreateUserInput{DisplayName: "Unenrolled", Email: "unenrolled@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		UploadDir:           filepath.Join(dataDir, "uploads"),
		PasswordAuthEnabled: passwordAuthEnabled,
	}).Handler())
	t.Cleanup(server.Close)
	return server, st, enrolled, unenrolled
}

func passwordLogin(t *testing.T, serverURL, identifier, password string) (*http.Response, string) {
	t.Helper()
	return passwordLoginWithHeaders(t, serverURL, identifier, password, nil)
}

func passwordLoginWithHeaders(t *testing.T, serverURL, identifier, password string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	body := `{"identifier":"` + identifier + `","password":"` + password + `"}`
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/auth/password/login", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(payload)
}

func TestPasswordLoginMintsSessionCookie(t *testing.T) {
	t.Parallel()
	server, st, enrolled, _ := newPasswordTestServer(t, true)

	resp, body := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", resp.StatusCode, body)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cc_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected a session cookie, got %#v", resp.Cookies())
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("expected the session cookie to be HTTP-only")
	}
	// The minted session must be the same kind the magic-link path mints.
	user, err := st.GetSessionUser(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != enrolled.ID {
		t.Fatalf("expected the session to resolve to %s, got %s", enrolled.ID, user.ID)
	}
}

func TestPasswordLoginAcceptsHandleIdentifier(t *testing.T) {
	t.Parallel()
	server, st, enrolled, _ := newPasswordTestServer(t, true)
	ctx := context.Background()
	if _, err := st.UpdateUserProfile(ctx, store.UpdateUserProfileInput{
		UserID:      enrolled.ID,
		DisplayName: enrolled.DisplayName,
		Handle:      "maggie",
	}); err != nil {
		t.Fatal(err)
	}

	resp, body := passwordLogin(t, server.URL, "Maggie", passwordTestSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected a case-insensitive handle to sign in, got %d %s", resp.StatusCode, body)
	}
}

func TestPasswordLoginRejectsWrongPasswordUnknownAndUnenrolled(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	wrongResp, wrongBody := passwordLogin(t, server.URL, "enrolled@example.com", "not the password")
	unknownResp, unknownBody := passwordLogin(t, server.URL, "nobody@example.com", passwordTestSecret)
	unenrolledResp, unenrolledBody := passwordLogin(t, server.URL, "unenrolled@example.com", passwordTestSecret)

	for _, resp := range []*http.Response{wrongResp, unknownResp, unenrolledResp} {
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Fatalf("expected no cookie on a rejected login, got %#v", resp.Cookies())
		}
	}
	// A uniform body is what keeps the endpoint from disclosing which
	// accounts exist or which ones have a password on file.
	if wrongBody != unknownBody || wrongBody != unenrolledBody {
		t.Fatalf("expected uniform rejection bodies, got %q %q %q", wrongBody, unknownBody, unenrolledBody)
	}
}

func TestPasswordLoginRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	if resp, _ := passwordLogin(t, server.URL, "", passwordTestSecret); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing identifier, got %d", resp.StatusCode)
	}
	if resp, _ := passwordLogin(t, server.URL, "enrolled@example.com", ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing password, got %d", resp.StatusCode)
	}
}

func TestPasswordLoginDisabledReportsNotImplemented(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, false)

	resp, body := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501 when password auth is off, got %d %s", resp.StatusCode, body)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("expected no cookie from a disabled endpoint, got %#v", resp.Cookies())
	}
}

func TestPasswordLoginRejectsCrossSiteRequests(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	crossOrigin, _ := passwordLoginWithHeaders(t, server.URL, "enrolled@example.com", passwordTestSecret,
		map[string]string{"Origin": "https://evil.example.com"})
	if crossOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-origin login, got %d", crossOrigin.StatusCode)
	}
	crossSite, _ := passwordLoginWithHeaders(t, server.URL, "enrolled@example.com", passwordTestSecret,
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if crossSite.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-site fetch, got %d", crossSite.StatusCode)
	}
	wrongType, _ := passwordLoginWithHeaders(t, server.URL, "enrolled@example.com", passwordTestSecret,
		map[string]string{"Content-Type": "text/plain"})
	if wrongType.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for a non-JSON login, got %d", wrongType.StatusCode)
	}
}

func TestPasswordLoginRateLimitsPerIdentifier(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	for i := 0; i < passwordLoginIDLimit; i++ {
		resp, body := passwordLogin(t, server.URL, "enrolled@example.com", "wrong password")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d %s", i, resp.StatusCode, body)
		}
	}
	resp, _ := passwordLogin(t, server.URL, "enrolled@example.com", "wrong password")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after the per-identifier budget, got %d", resp.StatusCode)
	}
	// The lockout must outlast a correct password, otherwise it only slows an
	// attacker down until the moment they guess right.
	correct, _ := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if correct.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the lockout to apply to correct passwords too, got %d", correct.StatusCode)
	}
	// A locked-out identifier must not have consumed the whole address budget
	// on the way, or one account could lock out every other account.
	if remaining := passwordLoginIPLimit - passwordLoginIDLimit - 2; remaining < 1 {
		t.Fatalf("test assumes the address budget outlasts one identifier lockout")
	}
	// A different identifier keeps its own budget.
	other, _ := passwordLogin(t, server.URL, "unenrolled@example.com", passwordTestSecret)
	if other.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a separate budget per identifier, got %d", other.StatusCode)
	}
}

func TestPasswordLoginSuccessDoesNotSpendTheAccountBudget(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	// Signing in correctly more times than the account budget allows must
	// never lock the account owner out of their own account.
	for i := 0; i < passwordLoginIDLimit+2; i++ {
		resp, body := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sign-in %d: expected 200, got %d %s", i, resp.StatusCode, body)
		}
	}
	// Failures still count, and the budget is intact because none were spent.
	for i := 0; i < passwordLoginIDLimit; i++ {
		if resp, _ := passwordLogin(t, server.URL, "enrolled@example.com", "wrong password"); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failure %d: expected 401, got %d", i, resp.StatusCode)
		}
	}
	if resp, _ := passwordLogin(t, server.URL, "enrolled@example.com", "wrong password"); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected failures to still trip the lockout, got %d", resp.StatusCode)
	}
}

func TestPasswordLoginIdentifierBudgetIgnoresCasing(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)

	for i := 0; i < passwordLoginIDLimit; i++ {
		passwordLogin(t, server.URL, "Enrolled@Example.com", "wrong password")
	}
	resp, _ := passwordLogin(t, server.URL, "enrolled@example.com", "wrong password")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected casing variants to share one budget, got %d", resp.StatusCode)
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	t.Parallel()
	server, st, _, _ := newPasswordTestServer(t, true)

	resp, body := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected the login to succeed, got %d %s", resp.StatusCode, body)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cc_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected a session cookie")
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, "1")
	req.AddCookie(sessionCookie)
	logoutResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from logout, got %d", logoutResp.StatusCode)
	}
	if _, err := st.GetSessionUser(context.Background(), sessionCookie.Value); err == nil {
		t.Fatal("expected the revoked session to stop resolving")
	}
	// Signing out twice must not fail: the browser may retry.
	repeat, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/logout", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	repeat.Header.Set("Content-Type", "application/json")
	repeat.Header.Set(csrfHeaderName, "1")
	repeat.AddCookie(sessionCookie)
	repeatResp, err := http.DefaultClient.Do(repeat)
	if err != nil {
		t.Fatal(err)
	}
	defer repeatResp.Body.Close()
	if repeatResp.StatusCode != http.StatusOK {
		t.Fatalf("expected sign-out to be idempotent, got %d", repeatResp.StatusCode)
	}
}

func TestLimiterBlockedAndRecordSeparateCheckingFromCharging(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	limiter := newSlidingWindowLimiter(2, time.Minute)
	limiter.nowFn = func() time.Time { return now }

	// Checking never charges, however often it is called.
	for i := 0; i < 10; i++ {
		if limiter.blocked("account") {
			t.Fatal("expected checking alone to leave the budget untouched")
		}
	}
	limiter.record("account")
	if limiter.blocked("account") {
		t.Fatal("expected one charge to stay under a limit of two")
	}
	limiter.record("account")
	if !limiter.blocked("account") {
		t.Fatal("expected the budget to be spent after two charges")
	}
	now = now.Add(time.Minute)
	if limiter.blocked("account") {
		t.Fatal("expected the window to release the key")
	}
}

func TestEnabledAuthMethodsReflectConfiguration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		options  Options
		expected []string
	}{
		{name: "none", options: Options{}, expected: []string{}},
		{name: "password only", options: Options{PasswordAuthEnabled: true}, expected: []string{"password"}},
		{
			name:     "github only",
			options:  Options{GitHubOAuth: GitHubOAuthConfig{ClientID: "id", ClientSecret: "secret"}},
			expected: []string{"github"},
		},
		{
			name:     "both",
			options:  Options{PasswordAuthEnabled: true, GitHubOAuth: GitHubOAuthConfig{ClientID: "id", ClientSecret: "secret"}},
			expected: []string{"github", "password"},
		},
		{
			name:     "github half configured",
			options:  Options{GitHubOAuth: GitHubOAuthConfig{ClientID: "id"}},
			expected: []string{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			methods := New(nil, realtime.NewHub(), testCase.options).enabledAuthMethods()
			if strings.Join(methods, ",") != strings.Join(testCase.expected, ",") {
				t.Fatalf("expected %v, got %v", testCase.expected, methods)
			}
		})
	}
}
