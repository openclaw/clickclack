package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

type clientOptions struct {
	Server    string `json:"server"`
	Token     string `json:"token,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Channel   string `json:"channel,omitempty"`
	JSON      bool   `json:"-"`
	Plain     bool   `json:"-"`
	NoInput   bool   `json:"-"`
	Verbose   bool   `json:"-"`
}

type clientConfig struct {
	Server    string `json:"server,omitempty"`
	Token     string `json:"token,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

type apiClient struct {
	opts     clientOptions
	defaults clientDefaults
	http     *http.Client
}

type clientDefaults struct {
	opts      clientOptions
	config    clientConfig
	hasConfig bool
}

func client(args []string) error {
	defaults := defaultClientOptions()
	opts := defaults.opts
	flags := flag.NewFlagSet("clickclack", flag.ExitOnError)
	addClientFlags(flags, &opts)
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		return errors.New("client command is required")
	}
	c := apiClient{opts: opts, defaults: defaults, http: &http.Client{Timeout: 10 * time.Second}}
	switch rest[0] {
	case "login":
		return c.login(rest[1:])
	case "logout":
		return c.logout(rest[1:])
	case "whoami":
		return c.whoami(rest[1:])
	case "status":
		return c.status(rest[1:])
	case "workspaces":
		return c.workspaces(rest[1:])
	case "channels":
		return c.channels(rest[1:])
	case "send":
		return c.send(rest[1:])
	case "messages":
		return c.messages(rest[1:])
	case "threads", "thread":
		return c.threads(rest[1:])
	case "reply":
		if len(rest) < 2 {
			return errors.New("usage: clickclack reply <message-id> [body]")
		}
		return c.threadReply(rest[1], rest[2:])
	default:
		return fmt.Errorf("unknown command %q", rest[0])
	}
}

func defaultClientOptions() clientDefaults {
	opts := clientOptions{
		Server:    getenvDefault("CLICKCLACK_SERVER", "http://localhost:8080"),
		Token:     os.Getenv("CLICKCLACK_TOKEN"),
		UserID:    os.Getenv("CLICKCLACK_USER_ID"),
		Workspace: os.Getenv("CLICKCLACK_WORKSPACE"),
		Channel:   os.Getenv("CLICKCLACK_CHANNEL"),
	}
	defaults := clientDefaults{opts: opts}
	cfg, err := loadClientConfig()
	if err == nil {
		defaults.config = cfg
		defaults.hasConfig = true
		if os.Getenv("CLICKCLACK_SERVER") == "" && cfg.Server != "" {
			opts.Server = cfg.Server
		}
	}
	defaults.opts = opts
	return defaults
}

func applyStoredDefaults(opts *clientOptions, defaults clientDefaults) {
	if !defaults.hasConfig || !sameServer(opts.Server, defaults.config.Server) {
		return
	}
	if opts.Token == "" && opts.UserID == "" {
		opts.Token = defaults.config.Token
	}
	if opts.Workspace == "" {
		opts.Workspace = defaults.config.Workspace
	}
	if opts.Channel == "" {
		opts.Channel = defaults.config.Channel
	}
}

func sameServer(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftURL, leftErr := url.Parse(strings.TrimRight(left, "/"))
	rightURL, rightErr := url.Parse(strings.TrimRight(right, "/"))
	if leftErr == nil && rightErr == nil && leftURL.Scheme != "" && rightURL.Scheme != "" {
		return strings.EqualFold(leftURL.Scheme, rightURL.Scheme) && strings.EqualFold(leftURL.Host, rightURL.Host) && strings.TrimRight(leftURL.Path, "/") == strings.TrimRight(rightURL.Path, "/")
	}
	return strings.TrimRight(left, "/") == strings.TrimRight(right, "/")
}

func addClientFlags(flags *flag.FlagSet, opts *clientOptions) {
	flags.StringVar(&opts.Server, "server", opts.Server, "ClickClack server URL")
	flags.StringVar(&opts.Token, "token", opts.Token, "session bearer token")
	flags.StringVar(&opts.UserID, "user", opts.UserID, "development user ID for X-ClickClack-User")
	flags.StringVar(&opts.UserID, "user-id", opts.UserID, "development user ID for X-ClickClack-User")
	flags.StringVar(&opts.Workspace, "workspace", opts.Workspace, "workspace id, slug, or name")
	flags.StringVar(&opts.Channel, "channel", opts.Channel, "channel id or name")
	flags.BoolVar(&opts.JSON, "json", opts.JSON, "emit JSON")
	flags.BoolVar(&opts.Plain, "plain", opts.Plain, "emit plain stable output")
	flags.BoolVar(&opts.NoInput, "no-input", opts.NoInput, "disable prompts")
	flags.BoolVar(&opts.Verbose, "verbose", opts.Verbose, "print diagnostics to stderr")
}

func (c apiClient) withOptions(opts clientOptions, useStoredToken bool) apiClient {
	if useStoredToken {
		applyStoredDefaults(&opts, c.defaults)
	}
	c.opts = opts
	return c
}

func (c apiClient) currentUser() (store.User, error) {
	var result struct {
		User store.User `json:"user"`
	}
	if err := c.get("/api/me", &result); err != nil {
		return store.User{}, err
	}
	return result.User, nil
}

func (c apiClient) listWorkspaces() ([]store.Workspace, error) {
	var result struct {
		Workspaces []store.Workspace `json:"workspaces"`
	}
	if err := c.get("/api/workspaces", &result); err != nil {
		return nil, err
	}
	return result.Workspaces, nil
}

func (c apiClient) resolveWorkspace() (store.Workspace, error) {
	items, err := c.listWorkspaces()
	if err != nil {
		return store.Workspace{}, err
	}
	needle := strings.TrimSpace(c.opts.Workspace)
	if needle == "" {
		if len(items) == 0 {
			return store.Workspace{}, errors.New("no workspaces visible")
		}
		return items[0], nil
	}
	for _, item := range items {
		if item.ID == needle || item.Slug == needle || strings.EqualFold(item.Name, needle) {
			return item, nil
		}
	}
	return store.Workspace{}, fmt.Errorf("workspace %q not found", needle)
}

func (c apiClient) listChannels(workspaceID string) ([]store.Channel, error) {
	var result struct {
		Channels []store.Channel `json:"channels"`
	}
	if err := c.get("/api/workspaces/"+url.PathEscape(workspaceID)+"/channels", &result); err != nil {
		return nil, err
	}
	return result.Channels, nil
}

func (c apiClient) resolveChannel() (store.Channel, error) {
	needle := strings.TrimSpace(c.opts.Channel)
	if strings.HasPrefix(needle, "chn_") {
		workspaces, err := c.channelSearchWorkspaces()
		if err != nil {
			return store.Channel{}, err
		}
		for _, workspace := range workspaces {
			items, err := c.listChannels(workspace.ID)
			if err != nil {
				return store.Channel{}, err
			}
			for _, item := range items {
				if item.ID == needle {
					return item, nil
				}
			}
		}
		return store.Channel{}, fmt.Errorf("channel %q not found", needle)
	}
	workspace, err := c.resolveWorkspace()
	if err != nil {
		return store.Channel{}, err
	}
	items, err := c.listChannels(workspace.ID)
	if err != nil {
		return store.Channel{}, err
	}
	if needle == "" {
		for _, item := range items {
			if item.Name == "general" {
				return item, nil
			}
		}
		if len(items) == 0 {
			return store.Channel{}, errors.New("no channels visible")
		}
		return items[0], nil
	}
	for _, item := range items {
		if item.ID == needle || item.Name == strings.TrimPrefix(needle, "#") {
			return item, nil
		}
	}
	return store.Channel{}, fmt.Errorf("channel %q not found", needle)
}

func (c apiClient) channelSearchWorkspaces() ([]store.Workspace, error) {
	if strings.TrimSpace(c.opts.Workspace) != "" {
		workspace, err := c.resolveWorkspace()
		if err != nil {
			return nil, err
		}
		return []store.Workspace{workspace}, nil
	}
	return c.listWorkspaces()
}

func (c apiClient) get(path string, out any) error {
	return c.doJSON(context.Background(), http.MethodGet, path, nil, out)
}

func (c apiClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	reqBody, err := encodeBody(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.Token)
	}
	if c.opts.UserID != "" {
		req.Header.Set("X-ClickClack-User", c.opts.UserID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &apiErr); err == nil && apiErr.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, apiErr.Error)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (c apiClient) url(path string) string {
	base := strings.TrimRight(c.opts.Server, "/")
	if strings.HasPrefix(path, "/") {
		return base + path
	}
	return base + "/" + path
}

func (c apiClient) write(jsonValue any, plain, human string) error {
	switch {
	case c.opts.JSON:
		return writeJSON(os.Stdout, jsonValue)
	case c.opts.Plain:
		if plain != "" {
			fmt.Fprintln(os.Stdout, plain)
		}
		return nil
	default:
		_, err := fmt.Fprint(os.Stdout, human)
		return err
	}
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func loadClientConfig() (clientConfig, error) {
	path, err := clientConfigPath()
	if err != nil {
		return clientConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return clientConfig{}, err
	}
	var cfg clientConfig
	return cfg, json.Unmarshal(data, &cfg)
}

func saveClientConfig(cfg clientConfig) error {
	path, err := clientConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeClientConfigFile(path, append(data, '\n'))
}

func writeClientConfigFile(path string, data []byte) error {
	target, err := resolveClientConfigTarget(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("client config target %q is not a regular file", target)
		}
		if info.Mode().Perm()&0o222 == 0 {
			return fmt.Errorf("client config target %q is not writable: %w", target, os.ErrPermission)
		}
		probe, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		if err := probe.Close(); err != nil {
			return err
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeClientConfigFileWithRename(target, data, mode, os.Rename)
}

func resolveClientConfigTarget(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve client config symlink %q: %w", path, err)
	}
	return resolved, nil
}

func writeClientConfigFileWithRename(target string, data []byte, mode os.FileMode, rename func(string, string) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	if err := rename(tmpPath, target); err != nil {
		return err
	}
	return nil
}

func clientConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clickclack", "config.json"), nil
}

func getenvDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
