package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openclaw/clickclack/apps/api/internal/store"
	"golang.org/x/oauth2"
)

// OpenClawIDConfig configures browser sign-in through the first-party
// OpenClaw ID OIDC provider (Better Auth OAuth 2.1 at id.openclaw.ai).
type OpenClawIDConfig struct {
	ClientID     string
	ClientSecret string
	Issuer       string
	PublicURL    string
	AuthURL      string
	TokenURL     string
	HTTPClient   *http.Client
}

const (
	defaultOpenClawIDIssuer      = "https://id.openclaw.ai/api/auth"
	defaultOpenClawIDHTTPTimeout = 30 * time.Second
	openClawIDTokenClockLeeway   = 30 * time.Second
)

const (
	openclawIDOAuthEventBrowserStart     = "browser_start"
	openclawIDOAuthEventStartRejected    = "start_rejected"
	openclawIDOAuthEventCapacityRejected = "capacity_rejected"
	openclawIDOAuthEventStateRejected    = "state_rejected"
	openclawIDOAuthEventProviderFailed   = "provider_failed"
	openclawIDOAuthEventIdentityRejected = "identity_rejected"
	openclawIDOAuthEventBrowserSucceeded = "browser_succeeded"
)

func (c OpenClawIDConfig) withDefaults() OpenClawIDConfig {
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.ClientSecret = strings.TrimSpace(c.ClientSecret)
	c.PublicURL = strings.TrimSpace(c.PublicURL)
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	if c.Issuer == "" {
		c.Issuer = defaultOpenClawIDIssuer
	}
	if c.AuthURL == "" {
		c.AuthURL = c.Issuer + "/oauth2/authorize"
	}
	if c.TokenURL == "" {
		c.TokenURL = c.Issuer + "/oauth2/token"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultOpenClawIDHTTPTimeout}
	}
	return c
}

func (s *Server) openclawIDStart(w http.ResponseWriter, r *http.Request) {
	s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventBrowserStart)
	if s.openclawID.ClientID == "" || s.openclawID.ClientSecret == "" {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStartRejected)
		writeError(w, http.StatusNotImplemented, errors.New("openclaw id sign-in is not configured"))
		return
	}
	redirectURL, err := s.openclawIDRedirectURL(r)
	if err != nil {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStartRejected)
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	browserBinding, err := s.oauthBrowserBinding(w, r)
	if err != nil {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStartRejected)
		if errors.Is(err, errAmbiguousCookie) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.writeOpenClawIDOAuthServerError(w, r, "browser binding", err)
		return
	}
	state, err := randomOAuthSecret()
	if err != nil {
		s.writeOpenClawIDOAuthServerError(w, r, "state generation", err)
		return
	}
	pkceVerifier, err := randomOAuthSecret()
	if err != nil {
		s.writeOpenClawIDOAuthServerError(w, r, "PKCE generation", err)
		return
	}
	now := time.Now().UTC()
	if err := s.store.CreateOAuthTransaction(r.Context(), store.OAuthTransaction{
		StateHash:          secretHash(state),
		BrowserBindingHash: secretHash(browserBinding),
		Mode:               store.OAuthModeBrowser,
		PKCEVerifier:       pkceVerifier,
		CreatedAt:          now,
		ExpiresAt:          now.Add(oauthTransactionTTL),
	}); err != nil {
		if errors.Is(err, store.ErrOAuthCapacityExceeded) {
			s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventCapacityRejected)
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStartRejected)
		s.writeOpenClawIDOAuthServerError(w, r, "transaction creation", err)
		return
	}
	http.Redirect(w, r, s.openclawIDOAuth2Config(redirectURL).AuthCodeURL(state, oauth2.S256ChallengeOption(pkceVerifier)), http.StatusFound)
}

func (s *Server) openclawIDCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if !validDesktopCode(state, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStateRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid openclaw id oauth state"))
		return
	}
	bindingCookie, err := requestCookie(r, s.cookies.OAuthBinding)
	if err != nil || !validDesktopCode(bindingCookie.Value, oauthEncodedSecretLength, oauthEncodedSecretLength) {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStateRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid openclaw id oauth state"))
		return
	}
	transaction, err := s.store.ConsumeOAuthTransaction(r.Context(), secretHash(state), secretHash(bindingCookie.Value), time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrOAuthTransactionInvalid) {
			s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStateRejected)
			writeError(w, http.StatusBadRequest, errors.New("invalid openclaw id oauth state"))
			return
		}
		s.writeOpenClawIDOAuthServerError(w, r, "transaction consumption", err)
		return
	}
	if transaction.Mode != store.OAuthModeBrowser {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventStateRejected)
		writeError(w, http.StatusBadRequest, errors.New("invalid openclaw id oauth state"))
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, errors.New("openclaw id oauth code is required"))
		return
	}
	redirectURL, err := s.openclawIDRedirectURL(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	idToken, err := s.exchangeOpenClawIDCode(r.Context(), code, transaction.PKCEVerifier, redirectURL)
	if err != nil {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventProviderFailed)
		s.writeOpenClawIDOAuthProviderError(w, r, "token exchange", err)
		return
	}
	claims, err := s.validateOpenClawIDToken(idToken)
	if err != nil {
		s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventIdentityRejected)
		writeError(w, http.StatusForbidden, err)
		return
	}
	user, err := s.store.GetOrCreateUserByEmail(r.Context(), "openclaw-id", claims.Email, firstNonEmpty(claims.Name, claims.Email))
	if err != nil {
		s.writeOpenClawIDOAuthServerError(w, r, "identity provisioning", err)
		return
	}
	if _, err := s.store.EnsureDefaultWorkspaceMember(r.Context(), user.ID); err != nil {
		s.writeOpenClawIDOAuthServerError(w, r, "workspace provisioning", err)
		return
	}
	session, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		s.writeOpenClawIDOAuthServerError(w, r, "browser session creation", err)
		return
	}
	s.setSessionCookie(w, r, session)
	s.recordOpenClawIDOAuthEvent(openclawIDOAuthEventBrowserSucceeded)
	destination := "/"
	if s.frontendURL != "" {
		destination = s.frontendURL + "/"
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

func (s *Server) exchangeOpenClawIDCode(ctx context.Context, code, verifier, redirectURL string) (string, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.openclawID.HTTPClient)
	token, err := s.openclawIDOAuth2Config(redirectURL).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", errors.New("openclaw id token exchange failed")
	}
	idToken, _ := token.Extra("id_token").(string)
	if strings.TrimSpace(idToken) == "" {
		return "", errors.New("openclaw id token missing")
	}
	return idToken, nil
}

func (s *Server) openclawIDOAuth2Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.openclawID.ClientID,
		ClientSecret: s.openclawID.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  s.openclawID.AuthURL,
			TokenURL: s.openclawID.TokenURL,
			// OpenClaw ID registers ClickClack as a confidential client
			// using client_secret_basic on the token endpoint.
			AuthStyle: oauth2.AuthStyleInHeader,
		},
		RedirectURL: redirectURL,
		Scopes:      []string{"openid", "profile", "email"},
	}
}

type openClawIDClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	jwt.RegisteredClaims
}

// validateOpenClawIDToken checks the id_token claims. The token arrives
// directly from the issuer over TLS on an authenticated confidential-client
// token exchange, so a local JWKS signature check is not required; issuer,
// audience, expiry, and verified email are still enforced.
func (s *Server) validateOpenClawIDToken(idToken string) (openClawIDClaims, error) {
	claims := openClawIDClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(strings.TrimSpace(idToken), &claims); err != nil {
		return openClawIDClaims{}, errors.New("invalid openclaw id token")
	}
	if strings.TrimRight(claims.Issuer, "/") != s.openclawID.Issuer {
		return openClawIDClaims{}, errors.New("invalid openclaw id token issuer")
	}
	if !slices.Contains(claims.Audience, s.openclawID.ClientID) {
		return openClawIDClaims{}, errors.New("invalid openclaw id token audience")
	}
	if claims.ExpiresAt == nil || time.Now().After(claims.ExpiresAt.Time.Add(openClawIDTokenClockLeeway)) {
		return openClawIDClaims{}, errors.New("openclaw id token expired")
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Email == "" || !claims.EmailVerified {
		return openClawIDClaims{}, errors.New("openclaw id account email is not verified")
	}
	return claims, nil
}

func (s *Server) openclawIDRedirectURL(r *http.Request) (string, error) {
	base := strings.TrimRight(firstNonEmpty(s.publicAPIURL, s.openclawID.PublicURL), "/")
	if base == "" {
		if s.disableDevAuth || !isLocalHostPort(r.Host) || !isLocalHostPort(r.RemoteAddr) {
			return "", errors.New("openclaw id sign-in requires a configured public URL")
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/api/auth/openclaw/callback", nil
}

func (s *Server) writeOpenClawIDOAuthServerError(w http.ResponseWriter, r *http.Request, phase string, err error) {
	log.Printf("openclaw id oauth %s failed correlation_id=%q error_type=%T", phase, correlationIDFromContext(r.Context()), err)
	writeError(w, http.StatusInternalServerError, errors.New("openclaw id oauth request failed"))
}

func (s *Server) writeOpenClawIDOAuthProviderError(w http.ResponseWriter, r *http.Request, phase string, err error) {
	log.Printf("openclaw id oauth provider %s failed correlation_id=%q error_type=%T", phase, correlationIDFromContext(r.Context()), err)
	writeError(w, http.StatusBadGateway, errors.New("openclaw id authentication provider request failed"))
}
