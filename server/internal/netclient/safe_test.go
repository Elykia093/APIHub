package netclient

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/elykia/apihub/server/internal/apperror"
)

func TestNormalizeBaseURL(t *testing.T) {
	got, err := NormalizeBaseURL("https://example.com/base/?ignored=1#fragment", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/base" {
		t.Fatalf("NormalizeBaseURL() = %q", got)
	}
	if joined := Join(got, "/api/status"); joined != "https://example.com/base/api/status" {
		t.Fatalf("Join() = %q", joined)
	}
}

func TestNormalizeBaseURLMatchesNodeURLCanonicalization(t *testing.T) {
	for _, test := range []struct{ input, expected string }{
		{"HTTPS://EXAMPLE.COM:443/a/../b/", "https://example.com/b"},
		{"https://example.com:444/a//b/", "https://example.com:444/a//b"},
		{"https://bücher.example/path", "https://xn--bcher-kva.example/path"},
		{"https://example.com/%7euser/", "https://example.com/%7euser"},
		{"https://example.com/a/./b/", "https://example.com/a/b"},
		{"https://134744072/path", "https://8.8.8.8/path"},
		{"https://010.010.010.010/path", "https://8.8.8.8/path"},
		{"https://0x08080808/path", "https://8.8.8.8/path"},
		{"https://8.8/path", "https://8.0.0.8/path"},
		{"https://2130706433/path", "https://127.0.0.1/path"},
		{"https://017700000001/path", "https://127.0.0.1/path"},
		{"https://0x7f000001/path", "https://127.0.0.1/path"},
		{"https://127.1/path", "https://127.0.0.1/path"},
		{"https://8.8.8.8/path", "https://8.8.8.8/path"},
		{"https://1.2.3.example/path", "https://1.2.3.example/path"},
	} {
		got, err := NormalizeBaseURL(test.input, false, true)
		if err != nil {
			t.Errorf("NormalizeBaseURL(%q) error = %v", test.input, err)
		} else if got != test.expected {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", test.input, got, test.expected)
		}
	}
}

func TestNormalizeBaseURLRejectsWHATWGNumericLoopbackForms(t *testing.T) {
	for _, host := range []string{"2130706433", "017700000001", "0x7f000001", "127.1"} {
		t.Run(host, func(t *testing.T) {
			_, err := NormalizeBaseURL("https://"+host, false, false)
			if appErr := apperror.As(err); appErr.Code != apperror.SiteURLBlocked {
				t.Fatalf("NormalizeBaseURL() error = %v, want %s", err, apperror.SiteURLBlocked)
			}
		})
	}
}

func TestNormalizeBaseURLRejectsInvalidWHATWGIPv4Numbers(t *testing.T) {
	for _, host := range []string{"4294967296", "1.2.3.256", "1.2.65536", "1.2.3.4.5", "09", "example.1", "1..2"} {
		t.Run(host, func(t *testing.T) {
			_, err := NormalizeBaseURL("https://"+host, false, true)
			if appErr := apperror.As(err); appErr.Code != apperror.ValidationError {
				t.Fatalf("NormalizeBaseURL() error = %v, want %s", err, apperror.ValidationError)
			}
		})
	}
}

func TestNormalizeBaseURLRejectsUnsafeTargets(t *testing.T) {
	for _, input := range []string{"http://example.com", "https://user:pass@example.com", "https://127.0.0.1", "https://[::1]", "https://169.254.169.254", "https://example.com/" + strings.Repeat("a", 2049)} {
		t.Run(input, func(t *testing.T) {
			if _, err := NormalizeBaseURL(input, false, false); err == nil {
				t.Fatalf("NormalizeBaseURL(%q) succeeded", input)
			}
		})
	}
}

func TestSpecialRangesMatchNodeIPAddrUnicastPolicy(t *testing.T) {
	addresses := []string{
		"192.88.99.1", "192.175.48.1", "192.31.196.1", "192.52.193.1",
		"100::1", "64:ff9b::7f00:1", "64:ff9b:1::1", "2001::1", "2002:7f00:1::1",
		"2620:4f:8000::1", "3fff::1", "5f00::1", "fec0::1",
	}
	for _, raw := range addresses {
		t.Run(raw, func(t *testing.T) {
			address := netip.MustParseAddr(raw)
			if isPublic(address) {
				t.Fatalf("isPublic(%s) = true", raw)
			}
			host := raw
			if address.Is6() {
				host = "[" + raw + "]"
			}
			if _, err := NormalizeBaseURL("https://"+host, false, false); err == nil {
				t.Fatalf("literal %s was allowed", raw)
			}
			client := &Client{
				lookupNetIP: func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{address}, nil
				},
				dialer: net.Dialer{},
			}
			_, err := client.dialContext(context.Background(), "tcp", "station.example:443")
			if appErr := apperror.As(err); appErr.Code != apperror.SiteURLBlocked {
				t.Fatalf("DNS result %s error = %v", raw, err)
			}
		})
	}
}
