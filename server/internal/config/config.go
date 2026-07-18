package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type Config struct {
	NodeEnv                   string
	Host                      string
	Port                      int
	DatabaseURL               string
	DatabasePoolMax           int
	DatabaseIdleTimeout       time.Duration
	DatabaseConnectionTimeout time.Duration
	DatabaseStatementTimeout  time.Duration
	AdminToken                string
	AppSecret                 string
	HTTPTimeout               time.Duration
	MaxResponseBytes          int64
	AllowPrivateSites         bool
	AllowInsecureHTTP         bool
}

func Load() (Config, error) {
	return LoadFrom(os.LookupEnv)
}

func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		NodeEnv:                   "development",
		Host:                      "127.0.0.1",
		Port:                      4180,
		DatabasePoolMax:           5,
		DatabaseIdleTimeout:       30 * time.Second,
		DatabaseConnectionTimeout: 5 * time.Second,
		DatabaseStatementTimeout:  15 * time.Second,
		HTTPTimeout:               10 * time.Second,
		MaxResponseBytes:          1024 * 1024,
	}
	if value, present := lookup("NODE_ENV"); present {
		cfg.NodeEnv = value
	}
	if value, present := lookup("HOST"); present {
		cfg.Host = strings.TrimSpace(value)
		if cfg.Host == "" {
			return Config{}, fmt.Errorf("HOST must contain at least one character")
		}
	}
	var err error
	if cfg.Port, err = ParsePort(lookup("PORT")); err != nil {
		return Config{}, err
	}
	if cfg.DatabasePoolMax, err = intValue(lookup, "DATABASE_POOL_MAX", cfg.DatabasePoolMax, 1, 20); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseIdleTimeout, err = durationMS(lookup, "DATABASE_IDLE_TIMEOUT_MS", cfg.DatabaseIdleTimeout, time.Second, 300*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseConnectionTimeout, err = durationMS(lookup, "DATABASE_CONNECTION_TIMEOUT_MS", cfg.DatabaseConnectionTimeout, time.Second, 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseStatementTimeout, err = durationMS(lookup, "DATABASE_STATEMENT_TIMEOUT_MS", cfg.DatabaseStatementTimeout, time.Second, 120*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.HTTPTimeout, err = durationMS(lookup, "HTTP_TIMEOUT_MS", cfg.HTTPTimeout, time.Second, 60*time.Second); err != nil {
		return Config{}, err
	}
	maxBytes, err := intValue(lookup, "MAX_RESPONSE_BYTES", int(cfg.MaxResponseBytes), 16*1024, 5*1024*1024)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxResponseBytes = int64(maxBytes)
	if cfg.AllowPrivateSites, err = boolValue(lookup, "ALLOW_PRIVATE_SITES", false); err != nil {
		return Config{}, err
	}
	if cfg.AllowInsecureHTTP, err = boolValue(lookup, "ALLOW_INSECURE_HTTP", false); err != nil {
		return Config{}, err
	}

	databaseURL, _ := lookup("DATABASE_URL")
	cfg.DatabaseURL = strings.TrimSpace(databaseURL)
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	parsed, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL connection URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "postgres" && scheme != "postgresql" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL connection URL")
	}
	cfg.AdminToken, _ = lookup("ADMIN_TOKEN")
	if jsStringLength(cfg.AdminToken) < 16 {
		return Config{}, fmt.Errorf("ADMIN_TOKEN must contain at least 16 characters")
	}
	cfg.AppSecret, _ = lookup("APP_SECRET")
	if jsStringLength(cfg.AppSecret) < 32 {
		return Config{}, fmt.Errorf("APP_SECRET must contain at least 32 characters")
	}
	if cfg.NodeEnv != "development" && cfg.NodeEnv != "test" && cfg.NodeEnv != "production" {
		return Config{}, fmt.Errorf("NODE_ENV must be development, test, or production")
	}
	return cfg, nil
}

func jsStringLength(value string) int { return len(utf16.Encode([]rune(value))) }

func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

func ParsePort(raw string, present bool) (int, error) {
	if !present {
		return 4180, nil
	}
	value, ok := ParseJavaScriptInteger(raw)
	if !ok || value < 1 || value > 65535 {
		return 0, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	return value, nil
}

func intValue(lookup func(string) (string, bool), key string, fallback, min, max int) (int, error) {
	raw, present := lookup(key)
	if !present {
		return fallback, nil
	}
	value, ok := ParseJavaScriptInteger(raw)
	if !ok || value < min || value > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, min, max)
	}
	return value, nil
}

// ParseJavaScriptInteger matches Number(value) followed by Number.isInteger for
// the integer forms accepted by APIHub's Node configuration and query schemas.
func ParseJavaScriptInteger(raw string) (int, bool) {
	text := strings.TrimFunc(raw, isJavaScriptWhitespace)
	if text == "" {
		return 0, true
	}
	lower := strings.ToLower(text)
	for prefix, base := range map[string]int{"0x": 16, "0b": 2, "0o": 8} {
		if strings.HasPrefix(lower, prefix) {
			value, err := strconv.ParseUint(lower[len(prefix):], base, 64)
			if err != nil || value > math.MaxInt {
				return 0, false
			}
			return int(value), true
		}
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt || number > math.MaxInt {
		return 0, false
	}
	return int(number), true
}

func isJavaScriptWhitespace(value rune) bool {
	switch value {
	case '\t', '\v', '\f', ' ', '\u00a0', '\ufeff', '\n', '\r', '\u2028', '\u2029', '\u1680', '\u202f', '\u205f', '\u3000':
		return true
	default:
		return value >= '\u2000' && value <= '\u200a'
	}
}

func durationMS(lookup func(string) (string, bool), key string, fallback, min, max time.Duration) (time.Duration, error) {
	value, err := intValue(lookup, key, int(fallback/time.Millisecond), int(min/time.Millisecond), int(max/time.Millisecond))
	return time.Duration(value) * time.Millisecond, err
}

func boolValue(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	value, present := lookup(key)
	if !present {
		return fallback, nil
	}
	raw := strings.TrimSpace(strings.ToLower(value))
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}
