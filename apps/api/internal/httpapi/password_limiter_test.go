package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/realtime"
	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type passwordAttemptStore struct {
	store.Store
	beforeVerify func() error
}

func (s *passwordAttemptStore) GetPasswordLogin(ctx context.Context, identifier string) (store.PasswordLogin, error) {
	login, err := s.Store.GetPasswordLogin(ctx, identifier)
	if err == nil {
		err = s.beforeVerify()
	}
	return login, err
}

func (s *passwordAttemptStore) GetUserPasswordHash(ctx context.Context, userID string) (string, error) {
	hash, err := s.Store.GetUserPasswordHash(ctx, userID)
	if err == nil {
		err = s.beforeVerify()
	}
	return hash, err
}

func passwordAttemptRequest(handler http.Handler, change bool, token, password string) int {
	path := "/api/auth/password/login"
	body := `{"identifier":"enrolled@example.com","password":"` + password + `"}`
	if change {
		path = "/api/auth/password/change"
		body = `{"current_password":"` + password + `","new_password":"` + changedPasswordSecret + `"}`
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp.Code
}

func TestPasswordAttemptsReserveBeforeVerification(t *testing.T) {
	for _, change := range []bool{false, true} {
		name := "login"
		limit := passwordLoginIDLimit
		if change {
			name, limit = "change", passwordChangeLimit
		}
		for _, success := range []bool{false, true} {
			outcome := "all_fail"
			if success {
				outcome = "one_success"
			}
			t.Run(name+"/"+outcome, func(t *testing.T) {
				_, st, enrolled, _ := newPasswordTestServer(t, true)
				session, err := st.CreateSession(context.Background(), enrolled.ID)
				if err != nil {
					t.Fatal(err)
				}
				// Zero marks a request paused on its snapshot, before Argon2. HTTP
				// status codes mark completed requests, so no timing guess is needed.
				events := make(chan int, 2*(limit+1))
				release := make(chan struct{})
				wrapped := &passwordAttemptStore{Store: st, beforeVerify: func() error {
					events <- 0
					<-release
					return nil
				}}
				server := New(wrapped, realtime.NewHub(), Options{PasswordAuthEnabled: true})
				handler := server.Handler()
				request := func(password string) int {
					return passwordAttemptRequest(handler, change, session.Token, password)
				}
				var workers sync.WaitGroup
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer func() { unblock(); workers.Wait() }()
				launch := func(password string) {
					workers.Go(func() { events <- request(password) })
				}
				watchdog := time.NewTimer(10 * time.Second)
				defer watchdog.Stop()
				next := func() int {
					t.Helper()
					select {
					case event := <-events:
						return event
					case <-watchdog.C:
						t.Fatal("request neither reached verification nor returned")
						return -1
					}
				}
				for i := 0; i < limit; i++ {
					password := "a wrong synthetic password"
					if success && i == 0 {
						password = passwordTestSecret
					}
					launch(password)
					if event := next(); event != 0 {
						t.Fatalf("attempt %d: expected verification, got HTTP %d", i, event)
					}
				}
				launch("another wrong synthetic password")
				if event := next(); event != http.StatusTooManyRequests {
					t.Fatalf("extra request passed the full in-flight budget: got event %d, want HTTP 429", event)
				}
				unblock()
				counts := map[int]int{}
				for i := 0; i < limit; i++ {
					counts[next()]++
				}
				wantSuccess := 0
				if success {
					wantSuccess = 1
				}
				if counts[http.StatusOK] != wantSuccess || counts[http.StatusUnauthorized] != limit-wantSuccess {
					t.Fatalf("unexpected completed attempt statuses: %v", counts)
				}
				if success {
					if code := request("a refunded slot's wrong password"); code != http.StatusUnauthorized {
						t.Fatalf("successful attempt did not refund its slot: HTTP %d", code)
					}
				}
				if code := request("a final wrong password"); code != http.StatusTooManyRequests {
					t.Fatalf("failed guesses did not exhaust the budget: HTTP %d", code)
				}
			})
		}
	}
}

func TestPasswordReadFailuresRefundReservation(t *testing.T) {
	for _, change := range []bool{false, true} {
		name, limit, want := "login", passwordLoginIDLimit, http.StatusUnauthorized
		if change {
			name, limit, want = "change", passwordChangeLimit, http.StatusBadRequest
		}
		t.Run(name, func(t *testing.T) {
			_, st, enrolled, _ := newPasswordTestServer(t, true)
			session, err := st.CreateSession(context.Background(), enrolled.ID)
			if err != nil {
				t.Fatal(err)
			}
			readErr := errors.New("temporary store read failure")
			wrapped := &passwordAttemptStore{Store: st, beforeVerify: func() error { return readErr }}
			handler := New(wrapped, realtime.NewHub(), Options{PasswordAuthEnabled: true}).Handler()
			for i := 0; i < limit+1; i++ {
				if code := passwordAttemptRequest(handler, change, session.Token, passwordTestSecret); code != want {
					t.Fatalf("read failure %d: got HTTP %d, want %d", i, code, want)
				}
			}
			readErr = nil
			if code := passwordAttemptRequest(handler, change, session.Token, passwordTestSecret); code != http.StatusOK {
				t.Fatalf("account stayed locked out after store recovery: HTTP %d", code)
			}
			for i := 0; i < limit; i++ {
				if code := passwordAttemptRequest(handler, change, session.Token, "a wrong synthetic password"); code != http.StatusUnauthorized {
					t.Fatalf("failure %d: expected refunded budget, got HTTP %d", i, code)
				}
			}
			if code := passwordAttemptRequest(handler, change, session.Token, "a wrong synthetic password"); code != http.StatusTooManyRequests {
				t.Fatalf("expected failed guesses to exhaust the recovered budget, got HTTP %d", code)
			}
		})
	}
}

type cancelPasswordAttemptStore struct {
	store.Store
	cancel context.CancelFunc
}

func (s *cancelPasswordAttemptStore) GetPasswordLogin(ctx context.Context, identifier string) (store.PasswordLogin, error) {
	login, err := s.Store.GetPasswordLogin(ctx, identifier)
	s.cancel()
	return login, err
}

func (s *cancelPasswordAttemptStore) GetUserPasswordHash(ctx context.Context, userID string) (string, error) {
	hash, err := s.Store.GetUserPasswordHash(ctx, userID)
	s.cancel()
	return hash, err
}

func TestPasswordDerivationCancellationRefundsReservation(t *testing.T) {
	for _, name := range []string{"login", "unknown", "unenrolled", "change"} {
		t.Run(name, func(t *testing.T) {
			_, st, enrolled, _ := newPasswordTestServer(t, true)
			session, err := st.CreateSession(t.Context(), enrolled.ID)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &cancelPasswordAttemptStore{Store: st}
			server := New(wrapped, realtime.NewHub(), Options{PasswordAuthEnabled: true})
			path, identifier := "/api/auth/password/login", "enrolled@example.com"
			limit := passwordLoginIDLimit
			if name == "unknown" || name == "unenrolled" {
				identifier = name + "@example.com"
			}
			body := `{"identifier":"` + identifier + `","password":"a wrong synthetic password"}`
			if name == "change" {
				path, limit = "/api/auth/password/change", passwordChangeLimit
				body = `{"current_password":"a wrong synthetic password","new_password":"` + changedPasswordSecret + `"}`
			}
			handler := server.Handler()
			for i := 0; i <= limit; i++ {
				ctx, cancel := context.WithCancel(t.Context())
				wrapped.cancel = cancel
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+session.Token)
				resp := httptest.NewRecorder()
				handler.ServeHTTP(resp, req)
				cancel()
				if resp.Code != http.StatusUnauthorized {
					t.Fatalf("canceled attempt %d: got HTTP %d, want 401 with a refunded reservation", i, resp.Code)
				}
			}
			limiter, key := server.passwordIDLimiter, identifier
			if name == "change" {
				limiter, key = server.passwordChangeLimiter, enrolled.ID
			}
			for i := 0; i < limit; i++ {
				if limiter.reserve(key) == nil {
					t.Fatalf("cancellation consumed failure-budget slot %d", i)
				}
			}
		})
	}
}
