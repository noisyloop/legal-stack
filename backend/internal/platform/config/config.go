// Package config loads and validates all runtime configuration from the
// environment (12-factor). The process refuses to start on invalid config:
// failing at boot is visible; failing on the first request is an outage.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment identifies the deployment environment. It gates behavior that
// must differ (cookie Secure flags in dev, log format), never security checks.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// Config is the validated runtime configuration for the API process.
type Config struct {
	Env Environment

	// HTTP server
	HTTPAddr          string
	ShutdownTimeout   time.Duration
	MaxRequestBodyMiB int64
	// TrustedProxies are CIDRs whose X-Forwarded-For headers are believed.
	// Empty means no proxy is trusted and the socket peer address is used —
	// trusting forwarded headers from arbitrary clients allows IP spoofing.
	TrustedProxies []netip.Prefix

	// Rate limiting (per client IP, token bucket)
	RateLimitRPS   float64
	RateLimitBurst int

	// Logging
	LogLevel  string // debug | info | warn | error
	LogFormat string // json | text

	// Dependencies
	DatabaseURL string
	RedisURL    string // optional in Phase 1
}

// Load reads configuration from the process environment and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:               Environment(getDefault("APP_ENV", string(EnvDevelopment))),
		HTTPAddr:          getDefault("HTTP_ADDR", ":8080"),
		LogLevel:          getDefault("LOG_LEVEL", "info"),
		LogFormat:         getDefault("LOG_FORMAT", "json"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          os.Getenv("REDIS_URL"),
		ShutdownTimeout:   15 * time.Second,
		MaxRequestBodyMiB: 1,
		RateLimitRPS:      20,
		RateLimitBurst:    40,
	}

	var err error
	if cfg.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxRequestBodyMiB, err = getInt64("MAX_REQUEST_BODY_MIB", cfg.MaxRequestBodyMiB); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitRPS, err = getFloat("RATE_LIMIT_RPS", cfg.RateLimitRPS); err != nil {
		return Config{}, err
	}
	if burst, err := getInt64("RATE_LIMIT_BURST", int64(cfg.RateLimitBurst)); err != nil {
		return Config{}, err
	} else {
		cfg.RateLimitBurst = int(burst)
	}
	if cfg.TrustedProxies, err = parseCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS")); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.Env {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("config: APP_ENV %q is not one of development|test|staging|production", c.Env)
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL is required")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: LOG_LEVEL %q is not one of debug|info|warn|error", c.LogLevel)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("config: LOG_FORMAT %q is not one of json|text", c.LogFormat)
	}
	if c.MaxRequestBodyMiB < 1 || c.MaxRequestBodyMiB > 64 {
		return fmt.Errorf("config: MAX_REQUEST_BODY_MIB must be within [1,64], got %d", c.MaxRequestBodyMiB)
	}
	if c.RateLimitRPS <= 0 || c.RateLimitBurst < 1 {
		return fmt.Errorf("config: rate limit values must be positive")
	}
	if c.ShutdownTimeout < time.Second {
		return fmt.Errorf("config: SHUTDOWN_TIMEOUT must be at least 1s")
	}
	return nil
}

// IsProduction reports whether the process runs with production posture.
// Staging deliberately counts as production: it must rehearse prod behavior.
func (c Config) IsProduction() bool {
	return c.Env == EnvProduction || c.Env == EnvStaging
}

func getDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return d, nil
}

func getInt64(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return n, nil
}

func getFloat(key string, fallback float64) (float64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return f, nil
}

func parseCIDRs(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS entry %q: %w", part, err)
		}
		out = append(out, p)
	}
	return out, nil
}
