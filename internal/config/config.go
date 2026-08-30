package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type AuthConfig struct {
	Mode string `yaml:"mode"`
}

type MediaConfig struct {
	LibraryRoot  string        `yaml:"library_root"`
	LinkMode     string        `yaml:"link_mode"`
	PathMappings []PathMapping `yaml:"path_mappings"`
}

type PathMapping struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type NamingConfig struct {
	ShowFolder    string `yaml:"show_folder"`
	Episode       string `yaml:"episode"`
	VersionSuffix string `yaml:"version_suffix"`
	Movie         string `yaml:"movie"`
}

type PolicyConfig struct {
	Movie                string  `yaml:"movie"`
	OVA                  string  `yaml:"ova"`
	NCOPNCED             string  `yaml:"ncop_nced"`
	PVCM                 string  `yaml:"pv_cm"`
	MultiVersion         string  `yaml:"multi_version"`
	AutoApproveThreshold float64 `yaml:"auto_approve_threshold"`
}

type LLMProviderConfig struct {
	Name         string `yaml:"name"`
	BaseURL      string `yaml:"base_url"`
	Model        string `yaml:"model"`
	APIKeyEnv    string `yaml:"api_key_env"`
	NativeSearch bool   `yaml:"native_search"`
	Priority     int    `yaml:"priority"`
}

type LLMConfig struct {
	Providers []LLMProviderConfig `yaml:"providers"`
}

type ArchiveIndexConfig struct {
	Enabled     bool   `yaml:"enabled"`
	AutoRefresh string `yaml:"auto_refresh"`
}

type BangumiConfig struct {
	Enabled      bool               `yaml:"enabled"`
	UserAgent    string             `yaml:"user_agent"`
	TokenEnv     string             `yaml:"token_env"`
	ArchiveIndex ArchiveIndexConfig `yaml:"archive_index"`
}

type AnilistConfig struct {
	Enabled bool `yaml:"enabled"`
}

type TMDBConfig struct {
	Enabled   bool   `yaml:"enabled"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type MetadataConfig struct {
	Bangumi BangumiConfig `yaml:"bangumi"`
	Anilist AnilistConfig `yaml:"anilist"`
	TMDB    TMDBConfig    `yaml:"tmdb"`
}

type ScannerConfig struct {
	RescanInterval int `yaml:"rescan_interval"`
}

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	DataDir  string         `yaml:"data_dir"`
	Media    MediaConfig    `yaml:"media"`
	Naming   NamingConfig   `yaml:"naming"`
	Policy   PolicyConfig   `yaml:"policy"`
	LLM      LLMConfig      `yaml:"llm"`
	Metadata MetadataConfig `yaml:"metadata"`
	Scanner  ScannerConfig  `yaml:"scanner"`
}

func Default() *Config {
	return &Config{
		Server:  ServerConfig{Host: "0.0.0.0", Port: 8080},
		Auth:    AuthConfig{Mode: "password"},
		DataDir: "./data",
		Media: MediaConfig{
			LibraryRoot: "/media/library",
			LinkMode:    "relative",
		},
		Naming: NamingConfig{
			ShowFolder:    "{title} ({year})",
			Episode:       "S{s:02}E{e:02} - {episode_title}",
			VersionSuffix: " [{version}]",
			Movie:         "{title} ({year})",
		},
		Policy: PolicyConfig{
			Movie:                "separate",
			OVA:                  "separate",
			NCOPNCED:             "s00",
			PVCM:                 "extras",
			MultiVersion:         "vault",
			AutoApproveThreshold: 0.90,
		},
		Metadata: MetadataConfig{
			Bangumi: BangumiConfig{
				Enabled:   true,
				UserAgent: "RyougiShiki-214/roxy (https://github.com/hinnyuu/roxy)",
				TokenEnv:  "ROXY_BGM_TOKEN",
				ArchiveIndex: ArchiveIndexConfig{
					Enabled:     true,
					AutoRefresh: "weekly",
				},
			},
			Anilist: AnilistConfig{Enabled: true},
			TMDB:    TMDBConfig{Enabled: true, APIKeyEnv: "ROXY_TMDB_KEY"},
		},
		Scanner: ScannerConfig{RescanInterval: 0},
	}
}

// Load 读取 YAML 配置（文件可缺省，缺省用默认值），随后应用 ROXY_* 环境变量覆盖。
// 环境变量覆盖约定见 docs/ARCHITECTURE.md §13。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read config %s: %w", path, err)
			}
		} else if err := yaml.Unmarshal(body, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	cfg.applyEnv()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func envStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func envInt(key string, dst *int) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func envFloat(key string, dst *float64) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			*dst = f
		}
	}
}

func (c *Config) applyEnv() {
	envStr("ROXY_SERVER_HOST", &c.Server.Host)
	envInt("ROXY_SERVER_PORT", &c.Server.Port)
	envStr("ROXY_DATA_DIR", &c.DataDir)
	envStr("ROXY_AUTH_MODE", &c.Auth.Mode)
	envStr("ROXY_MEDIA_LIBRARY_ROOT", &c.Media.LibraryRoot)
	envStr("ROXY_MEDIA_LINK_MODE", &c.Media.LinkMode)
	envFloat("ROXY_POLICY_AUTO_APPROVE_THRESHOLD", &c.Policy.AutoApproveThreshold)
	envStr("ROXY_POLICY_MULTI_VERSION", &c.Policy.MultiVersion)
	envInt("ROXY_SCANNER_RESCAN_INTERVAL", &c.Scanner.RescanInterval)
}

func (c *Config) validate() error {
	if c.Auth.Mode != "password" {
		return fmt.Errorf("auth.mode: 仅支持 password，得到 %q", c.Auth.Mode)
	}
	if c.Media.LinkMode != "relative" && c.Media.LinkMode != "absolute" {
		return fmt.Errorf("media.link_mode: relative|absolute，得到 %q", c.Media.LinkMode)
	}
	if c.Policy.MultiVersion != "vault" && c.Policy.MultiVersion != "tolerate" {
		return fmt.Errorf("policy.multi_version: vault|tolerate，得到 %q", c.Policy.MultiVersion)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	if c.Policy.AutoApproveThreshold < 0 || c.Policy.AutoApproveThreshold > 1 {
		return fmt.Errorf("policy.auto_approve_threshold 必须在 [0,1]: %v",
			c.Policy.AutoApproveThreshold)
	}
	return nil
}
