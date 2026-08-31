package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openclaw/clickclack/apps/api/internal/passwordauth"
	"github.com/openclaw/clickclack/apps/api/internal/realtime"
)

const changedPasswordSecret = "a different secret entirely"

// sessionCookieFrom returns the session cookie a response set, or nil.
func sessionCookieFrom(resp *http.Response) *http.Cookie {
	var found *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cc_session" {
			found = cookie
		}
	}
	return found
}

// signInForCookie signs the enrolled account in and returns its session cookie.
func signInForCookie(t *testing.T, serverURL string) *http.Cookie {
	t.Helper()
	resp, body := passwordLogin(t, serverURL, "enrolled@example.com", passwordTestSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected the login to succeed, got %d %s", resp.StatusCode, body)
	}
	cookie := sessionCookieFrom(resp)
	if cookie == nil {
		t.Fatal("expected a session cookie")
	}
	return cookie
}

// changePasswordRequest posts a change with the caller's own credentials. auth
// is applied to the request so a test can exercise the cookie path or the
// bearer path, and headers can override the defaults the browser would send.
func changePasswordRequest(t *testing.T, serverURL, current, next string, auth func(*http.Request), headers map[string]string) (*http.Response, string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"current_password": current, "new_password": next})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/auth/password/change", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, "1")
	if auth != nil {
		auth(req)
	}
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

func withCookie(cookie *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(cookie) }
}

func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func TestChangePasswordReplacesTheSecret(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(cookie), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from a valid change, got %d %s", resp.StatusCode, body)
	}

	oldResp, _ := passwordLogin(t, server.URL, "enrolled@example.com", passwordTestSecret)
	if oldResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected the old password to stop working, got %d", oldResp.StatusCode)
	}
	newResp, newBody := passwordLogin(t, server.URL, "enrolled@example.com", changedPasswordSecret)
	if newResp.StatusCode != http.StatusOK {
		t.Fatalf("expected the new password to sign in, got %d %s", newResp.StatusCode, newBody)
	}
}

func TestChangePasswordEndsOtherSessionsAndKeepsTheCurrentOne(t *testing.T) {
	t.Parallel()
	server, st, _, _ := newPasswordTestServer(t, true)
	ctx := context.Background()
	current := signInForCookie(t, server.URL)
	elsewhere := signInForCookie(t, server.URL)

	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(current), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from a valid change, got %d %s", resp.StatusCode, body)
	}

	// Changing a password is how someone locks out a device they no longer
	// control, so every other session has to end.
	if _, err := st.GetSessionUser(ctx, elsewhere.Value); err == nil {
		t.Fatal("expected the other session to be revoked")
	}
	// The tab that changed the password stays signed in.
	if _, err := st.GetSessionUser(ctx, current.Value); err != nil {
		t.Fatalf("expected the calling session to survive, got %v", err)
	}
}

func TestChangePasswordRejectsAWrongCurrentPassword(t *testing.T) {
	t.Parallel()
	server, st, enrolled, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	resp, _ := changePasswordRequest(t, server.URL, "not the password", changedPasswordSecret, withCookie(cookie), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a wrong current password, got %d", resp.StatusCode)
	}
	hash, err := st.GetUserPasswordHash(context.Background(), enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := passwordauth.Verify(hash, passwordTestSecret)
	if err != nil || !matched {
		t.Fatalf("expected the stored password to be untouched, matched=%v err=%v", matched, err)
	}
}

func TestChangePasswordRequiresAuthentication(t *testing.T) {
	t.Parallel()
	// The shared test server keeps the loopback dev-auth fallback on, which
	// resolves an anonymous local request to the first user. Disable it so the
	// request is genuinely unauthenticated.
	_, st, _, _ := newPasswordTestServer(t, true)
	server := httptest.NewServer(New(st, realtime.NewHub(), Options{
		PasswordAuthEnabled: true,
		DisableDevAuth:      true,
	}).Handler())
	t.Cleanup(server.Close)

	resp, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", resp.StatusCode)
	}
}

func TestChangePasswordRejectsAnUnenrolledAccount(t *testing.T) {
	t.Parallel()
	server, st, _, unenrolled := newPasswordTestServer(t, true)
	ctx := context.Background()
	session, err := st.CreateSession(ctx, unenrolled.ID)
	if err != nil {
		t.Fatal(err)
	}

	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withBearer(session.Token), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for an account with no password, got %d %s", resp.StatusCode, body)
	}
	// This endpoint must never enroll an account. Enabling password sign-in
	// stays an administrator action.
	hash, err := st.GetUserPasswordHash(ctx, unenrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "" {
		t.Fatal("expected the account to still have no password on file")
	}
}

func TestChangePasswordRejectsAWeakNewPassword(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	if resp, _ := changePasswordRequest(t, server.URL, passwordTestSecret, "short", withCookie(cookie), nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a too-short new password, got %d", resp.StatusCode)
	}
	if resp, _ := changePasswordRequest(t, server.URL, passwordTestSecret, "", withCookie(cookie), nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing new password, got %d", resp.StatusCode)
	}
	if resp, _ := changePasswordRequest(t, server.URL, "", changedPasswordSecret, withCookie(cookie), nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing current password, got %d", resp.StatusCode)
	}
}

func TestChangePasswordRateLimitsPerAccount(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	for i := 0; i < passwordChangeLimit; i++ {
		resp, body := changePasswordRequest(t, server.URL, "not the password", changedPasswordSecret, withCookie(cookie), nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d %s", i, resp.StatusCode, body)
		}
	}
	resp, _ := changePasswordRequest(t, server.URL, "not the password", changedPasswordSecret, withCookie(cookie), nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after the per-account budget, got %d", resp.StatusCode)
	}
	// The lockout has to outlast a correct password, or it only slows an
	// attacker holding a borrowed session down until they guess right.
	correct, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(cookie), nil)
	if correct.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the lockout to apply to a correct password too, got %d", correct.StatusCode)
	}
}

func TestChangePasswordSuccessDoesNotSpendTheAccountBudget(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	// Rotating the password more times than the budget allows must never lock
	// the account owner out of their own account.
	secrets := []string{passwordTestSecret, changedPasswordSecret}
	for i := 0; i < passwordChangeLimit+2; i++ {
		current, next := secrets[i%2], secrets[(i+1)%2]
		resp, body := changePasswordRequest(t, server.URL, current, next, withCookie(cookie), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("change %d: expected 200, got %d %s", i, resp.StatusCode, body)
		}
	}
}

func TestChangePasswordRejectsCrossSiteRequests(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newPasswordTestServer(t, true)
	cookie := signInForCookie(t, server.URL)

	crossOrigin, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(cookie),
		map[string]string{"Origin": "https://evil.example.com"})
	if crossOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-origin change, got %d", crossOrigin.StatusCode)
	}
	crossSite, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(cookie),
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if crossSite.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-site fetch, got %d", crossSite.StatusCode)
	}
	// A bearer caller skips the cookie CSRF middleware, so the handler's own
	// same-origin check is the one doing the work here.
	bearerCrossSite, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withBearer("sst_never_issued"),
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if bearerCrossSite.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for a cross-site bearer change, got %d", bearerCrossSite.StatusCode)
	}
	wrongType, _ := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withCookie(cookie),
		map[string]string{"Content-Type": "text/plain"})
	if wrongType.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for a non-JSON change, got %d", wrongType.StatusCode)
	}
	missingCSRF, err := http.NewRequest(http.MethodPost, server.URL+"/api/auth/password/change",
		strings.NewReader(`{"current_password":"`+passwordTestSecret+`","new_password":"`+changedPasswordSecret+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.AddCookie(cookie)
	missingCSRFResp, err := http.DefaultClient.Do(missingCSRF)
	if err != nil {
		t.Fatal(err)
	}
	defer missingCSRFResp.Body.Close()
	if missingCSRFResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without the CSRF header, got %d", missingCSRFResp.StatusCode)
	}
}

func TestChangePasswordDisabledReportsNotImplemented(t *testing.T) {
	t.Parallel()
	server, st, enrolled, _ := newPasswordTestServer(t, false)
	ctx := context.Background()
	session, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}

	resp, body := changePasswordRequest(t, server.URL, passwordTestSecret, changedPasswordSecret, withBearer(session.Token), nil)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected 501 when password auth is off, got %d %s", resp.StatusCode, body)
	}
	hash, err := st.GetUserPasswordHash(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	matched, err := passwordauth.Verify(hash, passwordTestSecret)
	if err != nil || !matched {
		t.Fatalf("expected a disabled endpoint to leave the password alone, matched=%v err=%v", matched, err)
	}
}

func TestCurrentUserReportsPasswordEnrollment(t *testing.T) {
	t.Parallel()
	server, st, enrolled, unenrolled := newPasswordTestServer(t, true)
	ctx := context.Background()

	enrolledSession, err := st.CreateSession(ctx, enrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enrolledFlag := currentUserPasswordEnrolled(t, server.URL, enrolledSession.Token); !enrolledFlag {
		t.Fatal("expected an enrolled account to be reported as enrolled")
	}
	unenrolledSession, err := st.CreateSession(ctx, unenrolled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unenrolledFlag := currentUserPasswordEnrolled(t, server.URL, unenrolledSession.Token); unenrolledFlag {
		t.Fatal("expected an account with no password to be reported as unenrolled")
	}
}

func currentUserPasswordEnrolled(t *testing.T, serverURL, token string) bool {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /api/me, got %d", resp.StatusCode)
	}
	var payload struct {
		User struct {
			PasswordEnrolled bool `json:"password_enrolled"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.User.PasswordEnrolled
}
