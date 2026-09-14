package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
	openClawIDDiscoveryMaxBytes  = 64 << 10
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
	c.Issuer = strings.TrimSpace(c.Issuer)
	if c.Issuer == "" {
		c.Issuer = defaultOpenClawIDIssuer
	}
	if c.AuthURL == "" {
		c.AuthURL = strings.TrimRight(c.Issuer, "/") + "/oauth2/authorize"
	}
	if c.TokenURL == "" {
		c.TokenURL = strings.TrimRight(c.Issuer, "/") + "/oauth2/token"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultOpenClawIDHTTPTimeout}
	}
	return c
}

type oidcDiscoveryDocument struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// ApplyDiscovery fills unset endpoints from issuer metadata, preserving each
// explicit override. Only the default OpenClaw ID issuer can fall back when
// discovery is unavailable; malformed metadata always fails closed.
func (c OpenClawIDConfig) ApplyDiscovery(ctx context.Context) (OpenClawIDConfig, error) {
	explicitAuth := strings.TrimSpace(c.AuthURL) != ""
	explicitToken := strings.TrimSpace(c.TokenURL) != ""
	c = c.withDefaults()
	if c.ClientID == "" || c.ClientSecret == "" {
		return c, nil
	}
	issuer, err := url.Parse(c.Issuer)
	if err != nil || !oidcHTTPURLAllowed(issuer) || issuer.RawQuery != "" || issuer.ForceQuery {
		return OpenClawIDConfig{}, errors.New("oidc issuer url is not allowed")
	}
	if explicitAuth && explicitToken {
		return c, nil
	}
	doc, err := c.fetchOIDCDiscovery(ctx)
	if err != nil {
		if c.Issuer == defaultOpenClawIDIssuer && errors.Is(err, errOIDCDiscoveryUnavailable) {
			return c, nil
		}
		return OpenClawIDConfig{}, err
	}
	if doc.Issuer != c.Issuer {
		return OpenClawIDConfig{}, errors.New("oidc discovery issuer does not match OPENCLAW_ID_ISSUER")
	}
	authURL, err := parseOIDCEndpoint(doc.AuthorizationEndpoint)
	if err != nil {
		return OpenClawIDConfig{}, fmt.Errorf("oidc authorization_endpoint: %w", err)
	}
	tokenURL, err := parseOIDCEndpoint(doc.TokenEndpoint)
	if err != nil {
		return OpenClawIDConfig{}, fmt.Errorf("oidc token_endpoint: %w", err)
	}
	if !explicitAuth {
		c.AuthURL = authURL
	}
	if !explicitToken {
		c.TokenURL = tokenURL
	}
	return c, nil
}

var errOIDCDiscoveryUnavailable = errors.New("oidc discovery unavailable")

func (c OpenClawIDConfig) fetchOIDCDiscovery(ctx context.Context) (oidcDiscoveryDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscoveryDocument{}, errors.New("oidc discovery request failed")
	}
	resp, err := discoveryHTTPClient(c.HTTPClient).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return oidcDiscoveryDocument{}, ctx.Err()
		}
		return oidcDiscoveryDocument{}, errOIDCDiscoveryUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return oidcDiscoveryDocument{}, errOIDCDiscoveryUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, openClawIDDiscoveryMaxBytes))
		return oidcDiscoveryDocument{}, fmt.Errorf("oidc discovery returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, openClawIDDiscoveryMaxBytes+1))
	if err != nil {
		return oidcDiscoveryDocument{}, errors.New("oidc discovery body unreadable")
	}
	if len(body) > openClawIDDiscoveryMaxBytes {
		return oidcDiscoveryDocument{}, errors.New("oidc discovery body too large")
	}
	var doc oidcDiscoveryDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return oidcDiscoveryDocument{}, errors.New("oidc discovery document is not json")
	}
	return doc, nil
}

func discoveryHTTPClient(base *http.Client) *http.Client {
	cloned := *base
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if cloned.Timeout <= 0 || cloned.Timeout > defaultOpenClawIDHTTPTimeout {
		cloned.Timeout = defaultOpenClawIDHTTPTimeout
	}
	return &cloned
}

func parseOIDCEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !oidcHTTPURLAllowed(parsed) {
		return "", errors.New("endpoint is not an allowed http(s) url")
	}
	return parsed.String(), nil
}

func oidcHTTPURLAllowed(parsed *url.URL) bool {
	if parsed == nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return isLocalHostPort(parsed.Host)
	default:
		return false
	}
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
	if claims.Issuer != s.openclawID.Issuer {
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
		if requestIsHTTPS(r) {
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
