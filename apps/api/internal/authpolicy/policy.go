package authpolicy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const MaxCookieNamespaceLength = 32

type CookieNames struct {
	Session      string
	OAuthBinding string
}

func DefaultCookieNames() CookieNames {
	return CookieNames{
		Session:      "cc_session",
		OAuthBinding: "cc_oauth_binding",
	}
}

func ParseCookieNamespace(input string) (string, error) {
	namespace := strings.TrimSpace(input)
	if namespace == "" {
		return "", nil
	}
	if len(namespace) > MaxCookieNamespaceLength {
		return "", fmt.Errorf("cookie namespace must be at most %d characters", MaxCookieNamespaceLength)
	}
	for index, character := range namespace {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if character == '-' && index > 0 && index < len(namespace)-1 {
			continue
		}
		return "", errors.New("cookie namespace must contain only lowercase letters, digits, and interior hyphens")
	}
	return namespace, nil
}

func NewCookieNames(namespace, publicURL string) (CookieNames, error) {
	namespace, err := ParseCookieNamespace(namespace)
	if err != nil {
		return CookieNames{}, err
	}
	if namespace == "" {
		return DefaultCookieNames(), nil
	}
	canonicalURL, err := CanonicalPublicURL(publicURL)
	if err != nil {
		return CookieNames{}, fmt.Errorf("namespaced cookies require a valid public URL: %w", err)
	}
	if canonicalURL == "" {
		return CookieNames{}, errors.New("namespaced cookies require CLICKCLACK_PUBLIC_URL")
	}
	prefix := "cc-" + namespace + "-"
	if strings.HasPrefix(canonicalURL, "https://") {
		prefix = "__Host-" + prefix
	}
	return CookieNames{
		Session:      prefix + "session",
		OAuthBinding: prefix + "oauth-binding",
	}, nil
}

func CanonicalPublicURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}
	value, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse public URL: %w", err)
	}
	if value.Scheme != "http" && value.Scheme != "https" {
		return "", errors.New("public URL must use http or https")
	}
	if value.Host == "" {
		return "", errors.New("public URL must include a host")
	}
	if value.User != nil {
		return "", errors.New("public URL must not include credentials")
	}
	if value.RawQuery != "" || value.Fragment != "" {
		return "", errors.New("public URL must not include a query or fragment")
	}
	if value.EscapedPath() != "" && value.EscapedPath() != "/" {
		return "", errors.New("public URL must be an origin without a path")
	}
	hostname := strings.ToLower(value.Hostname())
	if hostname == "" || strings.HasSuffix(hostname, ".") {
		return "", errors.New("public URL has an invalid host")
	}
	if value.Scheme == "http" && !isLoopbackHost(hostname) {
		return "", errors.New("non-loopback public URLs must use https")
	}
	port := value.Port()
	if port == defaultPort(value.Scheme) {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return value.Scheme + "://" + host, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func defaultPort(scheme string) string {
	if scheme == "http" {
		return "80"
	}
	return "443"
}
