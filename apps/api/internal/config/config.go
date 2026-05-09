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
	PublicURL          string `json:"public_url"`
	DevBootstrap       bool   `json:"dev_bootstrap"`
	GitHubClientID     string `json:"github_client_id"`
	GitHubClientSecret string `json:"github_client_secret"`
	GitHubAllowedOrg   string `json:"github_allowed_org"`
	PushoverAPIToken   string `json:"pushover_api_token"`
}

func Defaults() Config {
	return Config{Addr: ":8080", Data: "./data", DevBootstrap: true}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if env := os.Getenv("CLICKCLACK_ADDR"); env != "" {
		cfg.Addr = env
	}
	if env := os.Getenv("CLICKCLACK_DATA"); env != "" {
		cfg.Data = env
	}
	if env := os.Getenv("CLICKCLACK_DB"); env != "" {
		cfg.DB = env
	}
	if env := os.Getenv("CLICKCLACK_PUBLIC_URL"); env != "" {
		cfg.PublicURL = env
	}
	if env := os.Getenv("CLICKCLACK_DEV_BOOTSTRAP"); env != "" {
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
	if env := os.Getenv("CLICKCLACK_PUSHOVER_API_TOKEN"); env != "" {
		cfg.PushoverAPIToken = env
	}
	if path == "" {
		return cfg, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.Data == "" {
		cfg.Data = "./data"
	}
	return cfg, nil
}
