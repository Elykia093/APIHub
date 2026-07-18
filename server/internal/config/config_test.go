package config

import (
	"net"
	"strings"
	"testing"
)

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, present := values[key]
		return value, present
	}
}

func TestAddressAndPortSupportIPv6AndJavaScriptNumbers(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int
	}{
		{"1e3", 1000},
		{"4180.0", 4180},
	} {
		got, err := ParsePort(test.raw, true)
		if err != nil || got != test.want {
			t.Fatalf("ParsePort(%q) = %d, %v; want %d", test.raw, got, err, test.want)
		}
	}
	address := (Config{Host: "::1", Port: 4180}).Address()
	if address != net.JoinHostPort("::1", "4180") {
		t.Fatalf("Address() = %q", address)
	}
}

func TestJavaScriptIntegerWhitespaceMatchesNumberCoercion(t *testing.T) {
	if got, ok := ParseJavaScriptInteger("\ufeff1\ufeff"); !ok || got != 1 {
		t.Fatalf("BOM-wrapped integer = %d, %v; want 1, true", got, ok)
	}
	if got, ok := ParseJavaScriptInteger("\u00851\u0085"); ok {
		t.Fatalf("NEL-wrapped integer = %d, true; Node Number rejects it", got)
	}
}

func TestLoadFrom(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":        "postgresql://user:pass@db:5432/apihub",
		"ADMIN_TOKEN":         "test-admin-token-123456",
		"APP_SECRET":          "test-app-secret-that-is-at-least-32-characters",
		"ALLOW_PRIVATE_SITES": "true",
		"ALLOW_INSECURE_HTTP": "0",
	}
	cfg, err := LoadFrom(lookup(values))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Port != 4180 || !cfg.AllowPrivateSites || cfg.AllowInsecureHTTP {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadFromAcceptsCaseInsensitivePostgreSQLScheme(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "POSTGRESQL://user:pass@db:5432/apihub",
		"ADMIN_TOKEN":  "test-admin-token-123456",
		"APP_SECRET":   "test-app-secret-that-is-at-least-32-characters",
	}
	if _, err := LoadFrom(lookup(values)); err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
}

func TestLoadFromRejectsMissingSecretsAndNonPostgres(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgresql://db/apihub", "ADMIN_TOKEN": "test-admin-token-123456", "APP_SECRET": "test-app-secret-that-is-at-least-32-characters"}
	for _, test := range []struct{ name, key, value string }{
		{"missing admin token", "ADMIN_TOKEN", ""},
		{"missing app secret", "APP_SECRET", ""},
		{"non postgres", "DATABASE_URL", "mysql://db/apihub"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			if _, err := LoadFrom(lookup(values)); err == nil {
				t.Fatal("LoadFrom() succeeded, want error")
			}
		})
	}
}

func TestLoadFromUsesJavaScriptUTF16LengthForSecrets(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgresql://db/apihub",
		"ADMIN_TOKEN":  "test-admin-token-123456",
		"APP_SECRET":   "test-app-secret-that-is-at-least-32-characters",
	}
	for _, test := range []struct{ name, key, value string }{
		{"short multibyte admin token", "ADMIN_TOKEN", strings.Repeat("😀", 7)},
		{"short multibyte app secret", "APP_SECRET", strings.Repeat("😀", 15)},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			if _, err := LoadFrom(lookup(values)); err == nil {
				t.Fatal("LoadFrom() succeeded, want error")
			}
		})
	}
}

func TestLoadFromMatchesNodeEnvironmentCoercion(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgresql://db/apihub",
		"ADMIN_TOKEN":  "test-admin-token-123456",
		"APP_SECRET":   "test-app-secret-that-is-at-least-32-characters",
	}
	for _, test := range []struct {
		name, key, value string
		wantPort         int
		wantPool         int
		wantError        bool
	}{
		{"scientific port", "PORT", "1e3", 1000, 5, false},
		{"decimal integer pool", "DATABASE_POOL_MAX", "10.0", 4180, 10, false},
		{"empty port", "PORT", "", 0, 0, true},
		{"spaced node env", "NODE_ENV", " production ", 0, 0, true},
		{"empty host", "HOST", " ", 0, 0, true},
		{"empty boolean", "ALLOW_PRIVATE_SITES", "", 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			cfg, err := LoadFrom(lookup(values))
			if test.wantError {
				if err == nil {
					t.Fatalf("LoadFrom() succeeded: %+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Port != test.wantPort || cfg.DatabasePoolMax != test.wantPool {
				t.Fatalf("config = %+v", cfg)
			}
		})
	}
}
