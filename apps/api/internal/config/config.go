package config

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
	Addr               string `json:"addr"`
	Data               string `json:"data"`
	DB                 string `json:"db"`
	Uploads            string `json:"uploads"`
	PublicURL          string `json:"public_url"`
	DevBootstrap       bool   `json:"dev_bootstrap"`
	GitHubClientID     string `json:"github_client_id"`
	GitHubClientSecret string `json:"github_client_secret"`
	GitHubAllowedOrg   string `json:"github_allowed_org"`
	GitHubModeratorOrg string `json:"github_moderator_org"`
	PushoverAPIToken   string `json:"pushover_api_token"`
	R2AccountID        string `json:"r2_account_id"`
	R2AccessKeyID      string `json:"r2_access_key_id"`
	R2SecretAccessKey  string `json:"r2_secret_access_key"`
	R2Endpoint         string `json:"r2_endpoint"`
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
	if env := os.Getenv("CLICKCLACK_PUBLIC_URL"); env != "" {
		cfg.PublicURL = env
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
