package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const (
	callbackTimeout        = 3 * time.Second
	callbackResolveTimeout = 2 * time.Second
	maxCallbackBodyBytes   = 64 * 1024
)

var blockedCallbackIPPrefixes = []netip.Prefix{
	// IPv4 special-purpose and internal ranges.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// IPv6 special-purpose and internal ranges.
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type callbackResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type callbackNetworkPolicy struct {
	resolver    callbackResolver
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func newCallbackNetworkPolicy() *callbackNetworkPolicy {
	dialer := &net.Dialer{Timeout: callbackTimeout, KeepAlive: 30 * time.Second}
	return &callbackNetworkPolicy{
		resolver:    net.DefaultResolver,
		dialContext: dialer.DialContext,
	}
}

func (p *callbackNetworkPolicy) client() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dial,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   callbackTimeout,
		ResponseHeaderTimeout: callbackTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   callbackTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("callback redirects are not allowed")
		},
	}
}

func (p *callbackNetworkPolicy) validateURL(ctx context.Context, rawURL string) error {
	parsed, err := parseCallbackURL(rawURL)
	if err != nil {
		return err
	}
	resolveCtx, cancel := context.WithTimeout(ctx, callbackResolveTimeout)
	defer cancel()
	_, err = p.resolvePublicIPs(resolveCtx, parsed.Hostname())
	return err
}

func (p *callbackNetworkPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid callback address: %w", err)
	}
	ips, err := p.resolvePublicIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := p.dialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("callback host did not resolve")
	}
	return nil, lastErr
}

func (p *callbackNetworkPolicy) resolvePublicIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return nil, errors.New("callback_url host is required")
	}
	if isBlockedCallbackHostname(host) {
		return nil, errors.New("callback_url host is not public")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if !isPublicCallbackIP(ip) {
			return nil, errors.New("callback_url address is not public")
		}
		return []netip.Addr{ip}, nil
	}
	ips, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve callback_url host: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("callback_url host did not resolve")
	}
	out := make([]netip.Addr, 0, len(ips))
	seen := make(map[netip.Addr]struct{}, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		if !isPublicCallbackIP(ip) {
			return nil, errors.New("callback_url host resolves to a non-public address")
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	return out, nil
}

func parseCallbackURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("callback_url must be an http or https URL")
	}
	if parsed.User != nil {
		return nil, errors.New("callback_url must not include credentials")
	}
	return parsed, nil
}

func isBlockedCallbackHostname(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	switch host {
	case "metadata.google.internal", "metadata.goog", "instance-data", "instance-data.ec2.internal":
		return true
	default:
		return false
	}
}

func isPublicCallbackIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedCallbackIPPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func (s *Server) postEventCallback(ctx context.Context, subscription store.EventSubscription, event store.Event, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subscription.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Event-ID", event.ID)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(subscription.SigningSecret, timestamp, payload))
	return s.doCallback(req, "event subscription callback failed")
}

func (s *Server) postSlashCallback(ctx context.Context, command store.SlashCommand, payload []byte) (int, string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, command.CallbackURL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClickClack-Timestamp", timestamp)
	req.Header.Set("X-ClickClack-Signature", signSlashCallback(command.SigningSecret, timestamp, payload))
	return s.doCallback(req, "slash command callback failed")
}

func (s *Server) doCallback(req *http.Request, failureMessage string) (int, string, error) {
	resp, err := s.callbackClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCallbackBodyBytes))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, string(body), errors.New(failureMessage)
	}
	return resp.StatusCode, string(body), nil
}

func signSlashCallback(secret, timestamp string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
