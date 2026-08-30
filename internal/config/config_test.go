package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Media.LinkMode != "relative" {
		t.Errorf("default link_mode = %q, want relative", cfg.Media.LinkMode)
	}
	if cfg.Policy.AutoApproveThreshold != 0.90 {
		t.Errorf("default threshold = %v, want 0.90", cfg.Policy.AutoApproveThreshold)
	}
	if cfg.Policy.MultiVersion != "vault" {
		t.Errorf("default multi_version = %q, want vault", cfg.Policy.MultiVersion)
	}
	if !cfg.Policy.SeriesFirstConfirm {
		t.Error("default series_first_confirm must be true (D-043)")
	}
	if cfg.Metadata.Bangumi.UserAgent == "" {
		t.Error("bangumi user_agent must not be empty")
	}
}

func TestYAMLFileOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := []byte("server:\n  port: 9090\npolicy:\n  movie: s00\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Policy.Movie != "s00" {
		t.Errorf("policy.movie = %q, want s00", cfg.Policy.Movie)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("ROXY_SERVER_PORT", "7777")
	t.Setenv("ROXY_DATA_DIR", "/tmp/roxy-data")
	t.Setenv("ROXY_POLICY_AUTO_APPROVE_THRESHOLD", "0.85")
	t.Setenv("ROXY_POLICY_SERIES_FIRST_CONFIRM", "false")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("port = %d, want 7777 (env override)", cfg.Server.Port)
	}
	if cfg.DataDir != "/tmp/roxy-data" {
		t.Errorf("data_dir = %q, want /tmp/roxy-data", cfg.DataDir)
	}
	if cfg.Policy.AutoApproveThreshold != 0.85 {
		t.Errorf("threshold = %v, want 0.85", cfg.Policy.AutoApproveThreshold)
	}
	if cfg.Policy.SeriesFirstConfirm {
		t.Error("env must override series_first_confirm to false")
	}
}

func TestValidationRejectsBadValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("media:\n  link_mode: hardlink\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for link_mode=hardlink")
	}
}

func TestValidationRejectsBadMultiVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("policy:\n  multi_version: merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for multi_version=merge")
	}
}
