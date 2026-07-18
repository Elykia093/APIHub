package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

func localClient() *netclient.Client {
	return netclient.New(netclient.Options{Timeout: 2 * time.Second, MaxResponseBytes: 64 * 1024, AllowPrivateSites: true, AllowInsecureHTTP: true})
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}

func TestNewAPICheckinHeadersAndResult(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/status":
			writeJSON(t, w, map[string]any{"success": true, "data": map[string]any{"checkin_enabled": true}})
		case r.URL.Path == "/api/user/checkin" && r.Method == "GET":
			if r.Header.Get("Authorization") != "station-token" || r.Header.Get("New-Api-User") != "42" {
				t.Errorf("unexpected headers: %v", r.Header)
			}
			writeJSON(t, w, map[string]any{"success": true, "data": map[string]any{"stats": map[string]any{"checked_in_today": false}}})
		case r.URL.Path == "/api/user/checkin" && r.Method == "POST":
			posts.Add(1)
			writeJSON(t, w, map[string]any{"success": true, "message": "ok", "data": map[string]any{"quota_awarded": 123}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := localClient()
	defer client.Close()
	result, err := NewNewAPI(client).CheckIn(context.Background(), domain.SiteContext{BaseURL: server.URL, UserID: "42", AccessToken: "station-token"}, "2026-07-17")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "success" || result.RewardValue == nil || *result.RewardValue != 123 || posts.Load() != 1 {
		t.Fatalf("result=%+v posts=%d", result, posts.Load())
	}
}

func TestNewAPIStatusTransportErrorStopsCheckin(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/api/status" {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack status connection: %v", err)
				return
			}
			if err := connection.Close(); err != nil {
				t.Errorf("close hijacked connection: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"success": true, "data": map[string]any{"stats": map[string]any{"checked_in_today": true}}})
	}))
	defer server.Close()
	client := localClient()
	defer client.Close()

	_, err := NewNewAPI(client).CheckIn(context.Background(), domain.SiteContext{BaseURL: server.URL, UserID: "42", AccessToken: "station-token"}, "2026-07-17")
	if err == nil {
		t.Fatal("CheckIn() succeeded after /api/status transport error")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestZenAndSub2Adapters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer station-token" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/u/checkin":
			writeJSON(t, w, map[string]any{"success": true, "message": "checked", "data": map[string]any{"reward": 8}})
		case "/api/v1/announcements":
			writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"title": "Notice", "content": "Maintenance", "created_at": "2026-07-17T00:00:00Z"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := localClient()
	defer client.Close()
	site := domain.SiteContext{BaseURL: server.URL, AccessToken: "station-token"}
	checkin, err := NewZenAPI(client).CheckIn(context.Background(), site, "")
	if err != nil || checkin.Status != "success" {
		t.Fatalf("checkin=%+v err=%v", checkin, err)
	}
	announcements, err := NewSub2API(client).FetchAnnouncements(context.Background(), site)
	if err != nil || len(announcements.Items) != 1 || announcements.Items[0].Content != "Maintenance" {
		t.Fatalf("announcements=%+v err=%v", announcements, err)
	}
}

func TestDetectorPrefersZenEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/public/site-info":
			writeJSON(t, w, map[string]any{"data": map[string]any{"site_mode": "public"}})
		case "/api/v1/auth/me":
			writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{}})
		case "/api/status":
			writeJSON(t, w, map[string]any{"success": true, "data": map[string]any{}})
		}
	}))
	defer server.Close()
	client := localClient()
	defer client.Close()
	name, err := NewDetector(client).Detect(context.Background(), server.URL)
	if err != nil || name != domain.ZenAPI {
		t.Fatalf("Detect()=%q,%v", name, err)
	}
}

func TestSharedCompatibilityBoundaries(t *testing.T) {
	if headers := bearerHeaders("Bearer\tstation-token"); headers["Authorization"] != "Bearer\tstation-token" {
		t.Fatalf("Authorization = %q", headers["Authorization"])
	}
	if value, ok := safeInt64(float64(1<<53 - 1)); !ok || value != 1<<53-1 {
		t.Fatalf("maximum safe integer = %d, %v", value, ok)
	}
	if _, ok := safeInt64(float64(1 << 53)); ok {
		t.Fatal("unsafe JavaScript integer accepted")
	}
	expectedUnix := time.Unix(1_784_000_000, 0).UTC()
	for _, value := range []any{float64(1_784_000_000), "1784000000"} {
		parsed := normalizePublishedAt(value)
		if parsed == nil || !parsed.Equal(expectedUnix) {
			t.Errorf("normalizePublishedAt(%v) = %v, want %v", value, parsed, expectedUnix)
		}
	}
	dateOnly := normalizePublishedAt("2026-07-17")
	if dateOnly == nil || !dateOnly.Equal(time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("date-only publishedAt = %v", dateOnly)
	}
	if parsed := normalizePublishedAt("not-a-date"); parsed != nil {
		t.Fatalf("invalid publishedAt = %v", parsed)
	}
}
