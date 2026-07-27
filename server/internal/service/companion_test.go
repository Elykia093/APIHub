package service

import (
	"strings"
	"testing"
)

func TestValidateBrowserTarget(t *testing.T) {
	valid, err := validateBrowserTarget("https://example.com", "https://example.com/console/personal#section")
	if err != nil || valid != "https://example.com/console/personal" {
		t.Fatalf("valid target = %q, err = %v", valid, err)
	}
	valid, err = validateBrowserTarget("https://example.com", " HTTPS://EXAMPLE.COM:443/console/personal#section ")
	if err != nil || valid != "https://example.com/console/personal" {
		t.Fatalf("canonical target = %q, err = %v", valid, err)
	}
	valid, err = validateBrowserTarget("https://xn--fsqu00a.xn--0zwm56d", "https://例子.测试/console#section")
	if err != nil || valid != "https://xn--fsqu00a.xn--0zwm56d/console" {
		t.Fatalf("IDN target = %q, err = %v", valid, err)
	}
	for _, target := range []string{"https://other.example/path", "http://example.com/path", "https://user@example.com/path"} {
		if _, err := validateBrowserTarget("https://example.com", target); err == nil {
			t.Fatalf("target %q should be rejected", target)
		}
	}
}

func TestCreatePairingCode(t *testing.T) {
	first, expires, err := CreatePairingCode()
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := CreatePairingCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 24 || first != strings.ToUpper(first) || first == second || expires == "" {
		t.Fatalf("invalid pairing codes: %q %q expires=%q", first, second, expires)
	}
}
