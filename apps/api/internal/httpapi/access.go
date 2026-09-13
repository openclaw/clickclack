package httpapi

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	accessAssertionHeader = "Cf-Access-Jwt-Assertion"
	accessJWKSPath        = "/cdn-cgi/access/certs"
	accessFetchTimeout    = 5 * time.Second
	accessDefaultCacheTTL = time.Hour
	accessMaxCacheTTL     = 24 * time.Hour
	accessMaxJWKSBytes    = 1 << 20
	accessClockLeeway     = 30 * time.Second
	accessUnknownKidFloor = 30 * time.Second
)

type AccessConfig struct {
	TeamDomain string
	Audience   string
	HTTPClient *http.Client
	Now        func() time.Time
}

type accessVerifier struct {
	teamDomain string
	audience   string
	httpClient *http.Client
	now        func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
}

type accessClaims struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	CommonName string `json:"common_name"`
	jwt.RegisteredClaims
}

type accessJWKS struct {
	Keys []accessJWK `json:"keys"`
}

type accessJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type accessResponseWriterContextKey struct{}

func newAccessVerifier(config AccessConfig) *accessVerifier {
	teamDomain := strings.TrimSpace(config.TeamDomain)
	audience := strings.TrimSpace(config.Audience)
	parsed, err := url.Parse(teamDomain)
	if err != nil || audience == "" || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil
	}
	teamDomain = strings.TrimSuffix(teamDomain, "/")
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if clientCopy.Timeout == 0 || clientCopy.Timeout > accessFetchTimeout {
		clientCopy.Timeout = accessFetchTimeout
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &accessVerifier{
		teamDomain: teamDomain,
		audience:   audience,
		httpClient: &clientCopy,
		now:        now,
	}
}

func bindAccessResponseWriter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), accessResponseWriterContextKey{}, w)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func accessResponseWriter(r *http.Request) (http.ResponseWriter, bool) {
	w, ok := r.Context().Value(accessResponseWriterContextKey{}).(http.ResponseWriter)
	return w, ok
}

func (s *Server) accessActor(r *http.Request, assertion string) (actor, error) {
	claims, err := s.access.verify(r.Context(), assertion)
	if err != nil {
		return actor{}, err
	}
	displayName := firstNonEmpty(strings.TrimSpace(claims.Name), strings.TrimSpace(claims.CommonName), claims.Email)
	user, err := s.store.GetOrCreateUserByEmail(r.Context(), "cloudflare-access", claims.Email, displayName)
	if err != nil {
		return actor{}, err
	}
	if _, err := s.store.EnsureDefaultWorkspaceMember(r.Context(), user.ID); err != nil {
		return actor{}, err
	}
	session, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		return actor{}, err
	}
	w, ok := accessResponseWriter(r)
	if ok {
		s.setSessionCookie(w, r, session)
	}
	return actor{user: user}, nil
}

func (v *accessVerifier) verify(ctx context.Context, assertion string) (accessClaims, error) {
	claims := accessClaims{}
	token, err := jwt.ParseWithClaims(strings.TrimSpace(assertion), &claims, v.keyFunc(ctx),
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.teamDomain),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(accessClockLeeway),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil || !token.Valid {
		return accessClaims{}, errors.New("invalid access assertion")
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.IssuedAt == nil || claims.Email == "" {
		return accessClaims{}, errors.New("invalid access assertion")
	}
	return claims, nil
}

func (v *accessVerifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || strings.TrimSpace(kid) == "" {
			return nil, errors.New("access assertion key ID missing")
		}
		return v.key(ctx, kid)
	}
}

func (v *accessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	if now.Before(v.expires) {
		if key := v.keys[kid]; key != nil {
			return key, nil
		}
	}
	if v.keys[kid] == nil && !v.lastFetch.IsZero() && now.Sub(v.lastFetch) < accessUnknownKidFloor {
		// Bound upstream fetch amplification from forged assertions.
		return nil, errors.New("access assertion key not found")
	}
	keys, expires, err := v.fetchKeys(ctx, now)
	if err != nil {
		return nil, err
	}
	v.keys = keys
	v.expires = expires
	v.lastFetch = now
	key := keys[kid]
	if key == nil {
		return nil, errors.New("access assertion key not found")
	}
	return key, nil
}

func (v *accessVerifier) fetchKeys(ctx context.Context, now time.Time) (map[string]*rsa.PublicKey, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, accessFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.teamDomain+accessJWKSPath, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("access key endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, accessMaxJWKSBytes+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(body) > accessMaxJWKSBytes {
		return nil, time.Time{}, errors.New("access key response is too large")
	}
	var set accessJWKS
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, time.Time{}, err
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, encoded := range set.Keys {
		key, err := encoded.rsaPublicKey()
		if err != nil {
			continue
		}
		if _, duplicate := keys[encoded.Kid]; duplicate {
			return nil, time.Time{}, errors.New("access key endpoint returned a duplicate key ID")
		}
		keys[encoded.Kid] = key
	}
	if len(keys) == 0 {
		return nil, time.Time{}, errors.New("access key endpoint returned no usable RSA keys")
	}
	return keys, accessCacheExpiry(resp.Header, now), nil
}

func (j accessJWK) rsaPublicKey() (*rsa.PublicKey, error) {
	if strings.TrimSpace(j.Kid) == "" || j.Kty != "RSA" || (j.Alg != "" && j.Alg != "RS256") || (j.Use != "" && j.Use != "sig") {
		return nil, errors.New("unsupported access key")
	}
	modulus, err := base64.RawURLEncoding.DecodeString(j.N)
	if err != nil || len(modulus) == 0 {
		return nil, errors.New("invalid access key modulus")
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(j.E)
	if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
		return nil, errors.New("invalid access key exponent")
	}
	exponent := new(big.Int).SetBytes(exponentBytes)
	if !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > int64(^uint(0)>>1) || exponent.Int64()%2 == 0 {
		return nil, errors.New("invalid access key exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: int(exponent.Int64())}, nil
}

func accessCacheExpiry(header http.Header, now time.Time) time.Time {
	for directive := range strings.SplitSeq(header.Get("Cache-Control"), ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if !ok || !strings.EqualFold(name, "max-age") {
			continue
		}
		seconds, err := strconv.ParseInt(strings.Trim(value, "\""), 10, 64)
		if err == nil && seconds >= 0 {
			ttl := accessMaxCacheTTL
			if seconds > int64(accessMaxCacheTTL/time.Second) {
				return now.Add(ttl)
			}
			ttl = time.Duration(seconds) * time.Second
			return now.Add(ttl)
		}
	}
	if expires, err := http.ParseTime(header.Get("Expires")); err == nil && expires.After(now) {
		if expires.After(now.Add(accessMaxCacheTTL)) {
			return now.Add(accessMaxCacheTTL)
		}
		return expires
	}
	return now.Add(accessDefaultCacheTTL)
}
