package netclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/elykia/apihub/server/internal/apperror"
	"golang.org/x/net/idna"
)

type Options struct {
	Timeout                              time.Duration
	MaxResponseBytes                     int64
	AllowPrivateSites, AllowInsecureHTTP bool
}
type Response struct {
	Status int
	OK     bool
	Text   string
	JSON   any
}
type Client struct {
	options     Options
	http        *http.Client
	lookupNetIP func(context.Context, string, string) ([]netip.Addr, error)
	dialer      net.Dialer
}

func New(options Options) *Client {
	c := &Client{options: options, lookupNetIP: net.DefaultResolver.LookupNetIP, dialer: net.Dialer{Timeout: min(options.Timeout, 5*time.Second), KeepAlive: 30 * time.Second}}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           c.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   min(options.Timeout, 5*time.Second),
		ResponseHeaderTimeout: options.Timeout,
		ExpectContinueTimeout: time.Second,
	}
	c.http = &http.Client{Transport: transport, Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return c
}

func (c *Client) Close() {
	if transport, ok := c.http.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func NormalizeBaseURL(input string, allowInsecure, allowPrivate bool) (string, error) {
	if len(utf16.Encode([]rune(input))) > 2048 {
		return "", apperror.New(422, apperror.ValidationError, "baseUrl must contain at most 2048 characters", false)
	}
	parsed, err := url.Parse(input)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return "", apperror.New(422, apperror.ValidationError, "baseUrl must be a valid absolute URL", false)
	}
	if parsed.User != nil {
		return "", apperror.New(422, apperror.ValidationError, "baseUrl must not include credentials", false)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	schemeAllowed := parsed.Scheme == "https" || allowInsecure && parsed.Scheme == "http"
	if !schemeAllowed {
		return "", apperror.New(422, apperror.SiteURLBlocked, "Only HTTPS site URLs are allowed", false)
	}
	hostname, err := normalizeHostname(parsed.Hostname())
	if err != nil {
		return "", apperror.New(422, apperror.ValidationError, "baseUrl must be a valid absolute URL", false)
	}
	if address, err := netip.ParseAddr(hostname); err == nil && !allowPrivate && !isPublic(address.Unmap()) {
		return "", apperror.New(422, apperror.SiteURLBlocked, "Private or reserved IP addresses are not allowed", false)
	}
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	parsed.RawQuery, parsed.Fragment, parsed.RawFragment = "", "", ""
	parsed.ForceQuery = false
	if err := normalizeURLPath(parsed); err != nil {
		return "", apperror.New(422, apperror.ValidationError, "baseUrl must be a valid absolute URL", false)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeHostname(hostname string) (string, error) {
	if address, err := netip.ParseAddr(hostname); err == nil {
		return address.Unmap().String(), nil
	}
	hostname, err := idna.Lookup.ToASCII(strings.ToLower(hostname))
	if err != nil {
		return "", err
	}
	if address, candidate, err := parseWHATWGIPv4(hostname); candidate {
		if err != nil {
			return "", err
		}
		return address.String(), nil
	}
	return hostname, nil
}

func parseWHATWGIPv4(hostname string) (netip.Addr, bool, error) {
	parts := strings.Split(hostname, ".")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return netip.Addr{}, false, nil
	}
	if _, ok := parseWHATWGIPv4Number(parts[len(parts)-1]); !ok && !isASCIIDigits(parts[len(parts)-1]) {
		return netip.Addr{}, false, nil
	}
	if len(parts) > 4 {
		return netip.Addr{}, true, errors.New("invalid WHATWG IPv4 address")
	}

	numbers := make([]uint64, len(parts))
	for index, part := range parts {
		number, ok := parseWHATWGIPv4Number(part)
		if !ok || index < len(parts)-1 && number > 255 {
			return netip.Addr{}, true, errors.New("invalid WHATWG IPv4 address")
		}
		numbers[index] = number
	}
	lastLimit := uint64(1) << (8 * (5 - len(parts)))
	if numbers[len(numbers)-1] >= lastLimit {
		return netip.Addr{}, true, errors.New("invalid WHATWG IPv4 address")
	}

	value := numbers[len(numbers)-1]
	for index, number := range numbers[:len(numbers)-1] {
		value += number << (8 * (3 - index))
	}
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), true, nil
}

func parseWHATWGIPv4Number(part string) (uint64, bool) {
	if part == "" {
		return 0, false
	}
	base := 10
	digits := part
	if len(part) >= 2 && part[0] == '0' {
		base = 8
		digits = part[1:]
		if part[1] == 'x' || part[1] == 'X' {
			base = 16
			digits = part[2:]
		}
	}
	if digits == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(digits, base, 64)
	return value, err == nil
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func normalizeURLPath(parsed *url.URL) error {
	escaped := strings.TrimRight(removeDotSegments(parsed.EscapedPath()), "/")
	if escaped == "" {
		parsed.Path, parsed.RawPath = "", ""
		return nil
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return err
	}
	parsed.Path = decoded
	parsed.RawPath = ""
	if canonical := (&url.URL{Path: decoded}).EscapedPath(); escaped != canonical {
		parsed.RawPath = escaped
	}
	return nil
}

func removeDotSegments(escaped string) string {
	segments := strings.Split(escaped, "/")
	result := make([]string, 0, len(segments))
	for _, segment := range segments {
		dots := strings.ReplaceAll(strings.ToLower(segment), "%2e", ".")
		switch dots {
		case ".":
			continue
		case "..":
			if len(result) > 1 || len(result) == 1 && result[0] != "" {
				result = result[:len(result)-1]
			}
		default:
			result = append(result, segment)
		}
	}
	return strings.Join(result, "/")
}

func Join(baseURL, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (c *Client) RequestJSON(ctx context.Context, baseURL, path, method string, headers map[string]string, body any) (Response, error) {
	normalized, err := NormalizeBaseURL(baseURL, c.options.AllowInsecureHTTP, c.options.AllowPrivateSites)
	if err != nil {
		return Response{}, err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return Response{}, fmt.Errorf("encode upstream body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, Join(normalized, path), reader)
	if err != nil {
		return Response{}, fmt.Errorf("create upstream request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil || isTimeout(err) {
			return Response{}, apperror.Wrap(504, apperror.UpstreamTimeout, "Upstream request timed out", true, err)
		}
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return Response{}, appErr
		}
		return Response{}, apperror.Wrap(502, apperror.UpstreamRejected, "Unable to reach upstream site", true, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Response{}, apperror.New(502, apperror.UpstreamRedirectBlocked, "Upstream redirect was blocked", false)
	}
	limited := io.LimitReader(resp.Body, c.options.MaxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, apperror.Wrap(502, apperror.UpstreamRejected, "Unable to read upstream response", true, err)
	}
	if int64(len(payload)) > c.options.MaxResponseBytes {
		return Response{}, apperror.New(502, apperror.UpstreamResponseTooLarge, "Upstream response exceeded the configured limit", false)
	}
	var decoded any
	if len(bytes.TrimSpace(payload)) > 0 {
		_ = json.Unmarshal(payload, &decoded)
	}
	return Response{Status: resp.StatusCode, OK: resp.StatusCode >= 200 && resp.StatusCode < 300, Text: string(payload), JSON: decoded}, nil
}

func (c *Client) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split dial address: %w", err)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !c.options.AllowPrivateSites && !isPublic(ip.Unmap()) {
			return nil, apperror.New(422, apperror.SiteURLBlocked, "Site resolves to a private or reserved address", false)
		}
		return c.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	addresses, err := c.lookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve site hostname: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("site hostname did not resolve")
	}
	for _, address := range addresses {
		if !c.options.AllowPrivateSites && !isPublic(address.Unmap()) {
			return nil, apperror.New(422, apperror.SiteURLBlocked, "Site resolves to a private or reserved address", false)
		}
	}
	return c.dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

var deniedPrefixes = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.175.48.0/24",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"100::/64", "::ffff:0:0:0/96", "64:ff9b::/96", "64:ff9b:1::/48", "2001::/23",
	"2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20", "5f00::/16", "fec0::/10",
)

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}
func isPublic(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range deniedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
func isTimeout(err error) bool {
	if timeout, ok := err.(interface{ Timeout() bool }); ok {
		return timeout.Timeout()
	}
	return false
}
