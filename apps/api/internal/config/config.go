package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openclaw/clickclack/apps/api/internal/authpolicy"
)

type Config struct {
	Addr                   string   `json:"addr"`
	Data                   string   `json:"data"`
	DB                     string   `json:"db"`
	Uploads                string   `json:"uploads"`
	Environment            string   `json:"environment"`
	MetricsEnabled         bool     `json:"metrics_enabled"`
	PublicURL              string   `json:"public_url"`
	PublicAPIURL           string   `json:"public_api_url"`
	HomeURL                string   `json:"home_url"`
	HomeLabel              string   `json:"home_label"`
	EmbedFrameAncestors    []string `json:"embed_frame_ancestors"`
	CookieNamespace        string   `json:"cookie_namespace"`
	DevBootstrap           bool     `json:"dev_bootstrap"`
	GitHubClientID         string   `json:"github_client_id"`
	GitHubClientSecret     string   `json:"github_client_secret"`
	GitHubAllowedOrg       string   `json:"github_allowed_org"`
	GitHubModeratorOrg     string   `json:"github_moderator_org"`
	OpenClawIDClientID     string   `json:"openclaw_id_client_id"`
	OpenClawIDClientSecret string   `json:"openclaw_id_client_secret"`
	OpenClawIDIssuer       string   `json:"openclaw_id_issuer"`
	AccessTeamDomain       string   `json:"access_team_domain"`
	AccessAUD              string   `json:"access_aud"`
	PushoverAPIToken       string   `json:"pushover_api_token"`
	R2AccountID            string   `json:"r2_account_id"`
	R2AccessKeyID          string   `json:"r2_access_key_id"`
	R2SecretAccessKey      string   `json:"r2_secret_access_key"`
	R2Endpoint             string   `json:"r2_endpoint"`
}

func Defaults() Config {
	return Config{Addr: ":8080", Data: "./data", DevBootstrap: false}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	fileHasDevBootstrap := false
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			return Config{}, err
		}
		_, fileHasDevBootstrap = fields["dev_bootstrap"]
		if err := json.Unmarshal(body, &cfg); err != nil {
			return Config{}, err
		}
	}
	if env := os.Getenv("CLICKCLACK_ADDR"); env != "" {
		cfg.Addr = env
	}
	if env := os.Getenv("CLICKCLACK_DATA"); env != "" {
		cfg.Data = env
	}
	if env := os.Getenv("CLICKCLACK_DB"); env != "" {
		cfg.DB = env
	}
	if env := os.Getenv("CLICKCLACK_UPLOADS"); env != "" {
		cfg.Uploads = env
	}
	if env := os.Getenv("CLICKCLACK_ENVIRONMENT"); env != "" {
		cfg.Environment = env
	}
	if env := os.Getenv("CLICKCLACK_METRICS_ENABLED"); env != "" {
		value, err := strconv.ParseBool(env)
		if err != nil {
			return Config{}, err
		}
		cfg.MetricsEnabled = value
	}
	if env := os.Getenv("CLICKCLACK_PUBLIC_URL"); env != "" {
		cfg.PublicURL = env
	}
	if env := os.Getenv("CLICKCLACK_PUBLIC_API_URL"); env != "" {
		cfg.PublicAPIURL = env
	}
	if env := os.Getenv("CLICKCLACK_HOME_URL"); env != "" {
		cfg.HomeURL = env
	}
	if env := os.Getenv("CLICKCLACK_HOME_LABEL"); env != "" {
		cfg.HomeLabel = env
	}
	if env := os.Getenv("CLICKCLACK_EMBED_FRAME_ANCESTORS"); env != "" {
		cfg.EmbedFrameAncestors = ParseEmbedFrameAncestors(env)
	}
	if env := os.Getenv("CLICKCLACK_COOKIE_NAMESPACE"); env != "" {
		cfg.CookieNamespace = env
	}
	if env := os.Getenv("CLICKCLACK_DEV_BOOTSTRAP"); env != "" && !fileHasDevBootstrap {
		value, err := strconv.ParseBool(env)
		if err != nil {
			return Config{}, err
		}
		cfg.DevBootstrap = value
	}
	if env := os.Getenv("CLICKCLACK_GITHUB_CLIENT_ID"); env != "" {
		cfg.GitHubClientID = env
	}
	if env := os.Getenv("CLICKCLACK_GITHUB_CLIENT_SECRET"); env != "" {
		cfg.GitHubClientSecret = env
	}
	if env := os.Getenv("CLICKCLACK_GITHUB_ALLOWED_ORG"); env != "" {
		cfg.GitHubAllowedOrg = env
	}
	if env := os.Getenv("CLICKCLACK_GITHUB_MODERATOR_ORG"); env != "" {
		cfg.GitHubModeratorOrg = env
	}
	if env := os.Getenv("OPENCLAW_ID_CLIENT_ID"); env != "" {
		cfg.OpenClawIDClientID = env
	}
	if env := os.Getenv("OPENCLAW_ID_CLIENT_SECRET"); env != "" {
		cfg.OpenClawIDClientSecret = env
	}
	if env := os.Getenv("OPENCLAW_ID_ISSUER"); env != "" {
		cfg.OpenClawIDIssuer = env
	}
	if env := os.Getenv("CLICKCLACK_ACCESS_TEAM_DOMAIN"); env != "" {
		cfg.AccessTeamDomain = env
	}
	if env := os.Getenv("CLICKCLACK_ACCESS_AUD"); env != "" {
		cfg.AccessAUD = env
	}
	if env := os.Getenv("CLICKCLACK_PUSHOVER_API_TOKEN"); env != "" {
		cfg.PushoverAPIToken = env
	}
	if env := os.Getenv("CLICKCLACK_R2_ACCOUNT_ID"); env != "" {
		cfg.R2AccountID = env
	}
	if env := os.Getenv("CLICKCLACK_R2_ACCESS_KEY_ID"); env != "" {
		cfg.R2AccessKeyID = env
	}
	if env := os.Getenv("CLICKCLACK_R2_SECRET_ACCESS_KEY"); env != "" {
		cfg.R2SecretAccessKey = env
	}
	if env := os.Getenv("CLICKCLACK_R2_ENDPOINT"); env != "" {
		cfg.R2Endpoint = env
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.Data == "" {
		cfg.Data = "./data"
	}
	return cfg, nil
}

func (c *Config) ValidateServe() error {
	if err := normalizeAccessConfig(c); err != nil {
		return err
	}
	namespace, err := authpolicy.ParseCookieNamespace(c.CookieNamespace)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_COOKIE_NAMESPACE: %w", err)
	}
	publicURL, err := authpolicy.CanonicalPublicURL(c.PublicURL)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_PUBLIC_URL: %w", err)
	}
	publicAPIURL, err := authpolicy.CanonicalPublicAPIURL(c.PublicAPIURL)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_PUBLIC_API_URL: %w", err)
	}
	if publicAPIURL == "" {
		publicAPIURL = publicURL
	}
	embedFrameAncestors, err := normalizeEmbedFrameAncestors(c.EmbedFrameAncestors)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_EMBED_FRAME_ANCESTORS: %w", err)
	}
	if err := validatePublicURLPair(publicURL, publicAPIURL); err != nil {
		return err
	}
	clientID := strings.TrimSpace(c.GitHubClientID)
	clientSecret := strings.TrimSpace(c.GitHubClientSecret)
	allowedOrg := strings.TrimSpace(c.GitHubAllowedOrg)
	moderatorOrg := strings.TrimSpace(c.GitHubModeratorOrg)
	hasClientID := clientID != ""
	hasClientSecret := clientSecret != ""
	if hasClientID != hasClientSecret {
		return errors.New("CLICKCLACK_GITHUB_CLIENT_ID and CLICKCLACK_GITHUB_CLIENT_SECRET must be configured together")
	}
	if hasClientID && publicURL == "" {
		return errors.New("GitHub OAuth requires CLICKCLACK_PUBLIC_URL")
	}
	if (allowedOrg != "" || moderatorOrg != "") && !hasClientID {
		return errors.New("GitHub organization settings require GitHub OAuth credentials")
	}
	openclawClientID := strings.TrimSpace(c.OpenClawIDClientID)
	openclawClientSecret := strings.TrimSpace(c.OpenClawIDClientSecret)
	if (openclawClientID != "") != (openclawClientSecret != "") {
		return errors.New("OPENCLAW_ID_CLIENT_ID and OPENCLAW_ID_CLIENT_SECRET must be configured together")
	}
	if openclawClientID != "" && publicURL == "" {
		return errors.New("OpenClaw ID sign-in requires CLICKCLACK_PUBLIC_URL")
	}
	if _, err := authpolicy.NewCookieNames(namespace, publicURL, publicAPIURL); err != nil {
		return fmt.Errorf("cookie policy: %w", err)
	}
	homeURL, homeLabel, err := normalizeHomeLink(c.HomeURL, c.HomeLabel)
	if err != nil {
		return err
	}
	c.HomeURL = homeURL
	c.HomeLabel = homeLabel
	c.PublicAPIURL = publicAPIURL
	c.EmbedFrameAncestors = embedFrameAncestors
	c.CookieNamespace = namespace
	c.PublicURL = publicURL
	c.GitHubClientID = clientID
	c.GitHubClientSecret = clientSecret
	c.GitHubAllowedOrg = allowedOrg
	c.GitHubModeratorOrg = moderatorOrg
	c.OpenClawIDClientID = openclawClientID
	c.OpenClawIDClientSecret = openclawClientSecret
	c.OpenClawIDIssuer = strings.TrimSpace(c.OpenClawIDIssuer)
	return nil
}

// ParseEmbedFrameAncestors parses the comma- or whitespace-separated format
// accepted by CLICKCLACK_EMBED_FRAME_ANCESTORS and --embed-frame-ancestors.
func ParseEmbedFrameAncestors(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

func normalizeEmbedFrameAncestors(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" ||
			strings.Contains(parsed.Hostname(), "*") ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("%q must be an HTTP(S) origin without a path, query, or fragment", value)
		}
		origin := (&url.URL{Scheme: strings.ToLower(parsed.Scheme), Host: strings.ToLower(parsed.Host)}).String()
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}
	return normalized, nil
}

func validatePublicURLPair(publicURL, publicAPIURL string) error {
	if publicURL == "" || publicAPIURL == "" {
		return nil
	}
	frontend, err := authpolicy.CanonicalPublicURL(publicURL)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_PUBLIC_URL: %w", err)
	}
	api, err := authpolicy.CanonicalPublicAPIURL(publicAPIURL)
	if err != nil {
		return fmt.Errorf("CLICKCLACK_PUBLIC_API_URL: %w", err)
	}
	if strings.HasPrefix(frontend, "http://") || strings.HasPrefix(api, "http://") {
		frontendURL, _ := url.Parse(frontend)
		apiURL, _ := url.Parse(api)
		if frontendURL.Scheme != "http" || apiURL.Scheme != "http" || !strings.EqualFold(frontendURL.Hostname(), apiURL.Hostname()) {
			return errors.New("loopback split origins must both use HTTP and the same hostname")
		}
	}
	return nil
}

// MaxHomeLabelLength bounds the label rendered on the workspace rail's home
// button; it is a short badge, not a title.
const MaxHomeLabelLength = 32

func normalizeHomeLink(rawURL, rawLabel string) (string, string, error) {
	homeURL := strings.TrimSpace(rawURL)
	// Browsers treat backslashes as slashes and strip tabs/newlines before
	// parsing URLs. Reject them before trimming or parsing with net/url.
	if strings.ContainsFunc(rawURL, func(r rune) bool { return r <= 0x1f || r == 0x7f || r == '\\' }) {
		return "", "", errors.New("CLICKCLACK_HOME_URL must not contain control characters or backslashes")
	}
	if homeURL != "" {
		parsed, err := url.Parse(homeURL)
		if err != nil || parsed.User != nil {
			return "", "", errors.New("CLICKCLACK_HOME_URL must be an absolute http(s) URL without credentials or a path starting with /")
		}
		if strings.HasPrefix(homeURL, "/") {
			base := &url.URL{Scheme: "https", Host: "clickclack.invalid"}
			resolved := base.ResolveReference(parsed)
			if strings.HasPrefix(homeURL, "//") || resolved.Scheme != base.Scheme || resolved.Host != base.Host {
				return "", "", errors.New("CLICKCLACK_HOME_URL path must stay on the deployment origin")
			}
		} else if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return "", "", errors.New("CLICKCLACK_HOME_URL must be an absolute http(s) URL or a path starting with /")
		}
	}
	homeLabel := strings.TrimSpace(rawLabel)
	if utf8.RuneCountInString(homeLabel) > MaxHomeLabelLength {
		return "", "", fmt.Errorf("CLICKCLACK_HOME_LABEL must be at most %d characters", MaxHomeLabelLength)
	}
	return homeURL, homeLabel, nil
}
