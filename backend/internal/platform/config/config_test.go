package config

import (
	"strings"
	"testing"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://app:secret@localhost:5432/legalstack")
}

func TestLoadDefaults(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json", cfg.LogFormat)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want none by default (no header trust)", cfg.TrustedProxies)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("Load() error = %v, want DATABASE_URL required error", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name, key, value, wantErr string
	}{
		{"bad env", "APP_ENV", "prod", "APP_ENV"},
		{"bad log level", "LOG_LEVEL", "verbose", "LOG_LEVEL"},
		{"bad log format", "LOG_FORMAT", "xml", "LOG_FORMAT"},
		{"bad body limit", "MAX_REQUEST_BODY_MIB", "0", "MAX_REQUEST_BODY_MIB"},
		{"bad rps", "RATE_LIMIT_RPS", "-1", "rate limit"},
		{"bad cidr", "TRUSTED_PROXY_CIDRS", "10.0.0.1", "TRUSTED_PROXY_CIDRS"},
		{"bad duration", "SHUTDOWN_TIMEOUT", "soon", "SHUTDOWN_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() error = %v, want error mentioning %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadParsesTrustedProxies(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 192.168.1.0/24")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("TrustedProxies = %v, want 2 prefixes", cfg.TrustedProxies)
	}
}

func TestIsProduction(t *testing.T) {
	if !(Config{Env: EnvStaging}).IsProduction() {
		t.Error("staging must run with production posture")
	}
	if (Config{Env: EnvDevelopment}).IsProduction() {
		t.Error("development must not report production posture")
	}
}
