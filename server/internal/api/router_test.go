package api

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	adapterpkg "github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/config"
)

const testAdminToken = "test-admin-token-1234567890"

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	web := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>APIHub</title>")}, "assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok')")}}
	return NewRouter(Dependencies{Config: config.Config{AdminToken: testAdminToken}, Adapters: adapterpkg.NewRegistry(nil), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Web: fs.FS(web)})
}

func TestAuthenticationAndNotFoundBoundaries(t *testing.T) {
	router := testRouter(t)
	for _, test := range []struct {
		path, authorization string
		status              int
		code                string
	}{
		{"/api/v1/sites", "", 401, "AUTH_REQUIRED"},
		{"/api/v1/does-not-exist", "", 401, "AUTH_REQUIRED"},
		{"/api/v1/does-not-exist", "Bearer " + testAdminToken, 404, "NOT_FOUND"},
		{"/api/v1/does-not-exist", "Bearer  " + testAdminToken, 404, "NOT_FOUND"},
		{"/api/v1/does-not-exist", "Bearer " + testAdminToken + " ", 401, "AUTH_REQUIRED"},
		{"/health/does-not-exist", "", 404, "NOT_FOUND"},
	} {
		t.Run(test.path+test.authorization, func(t *testing.T) {
			request := httptest.NewRequest("GET", test.path, nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			var payload struct {
				Error struct {
					Code, RequestID string
					Retryable       bool
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != test.code || payload.Error.RequestID == "" {
				t.Fatalf("error = %+v", payload.Error)
			}
			if strings.HasPrefix(test.path, "/api/v1/") && response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestHeadAndTrailingSlashMatchNodeContract(t *testing.T) {
	router := testRouter(t)
	tests := []struct {
		method, target, authorization string
		status                        int
	}{
		{http.MethodHead, "/health/live", "", 200},
		{http.MethodHead, "/api/v1/site-adapters", "Bearer " + testAdminToken, 200},
		{http.MethodGet, "/health/live/", "", 404},
		{http.MethodGet, "/api/v1/sites/", "Bearer " + testAdminToken, 422},
		{http.MethodPost, "/api/v1/sites/", "Bearer " + testAdminToken, 404},
		{http.MethodPatch, "/api/v1/announcements/11111111-1111-1111-8111-111111111111/", "Bearer " + testAdminToken, 404},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestUnauthorizedRequestsDoNotConsumeGlobalRateLimit(t *testing.T) {
	router := testRouter(t)
	for attempt := 0; attempt < 245; attempt++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil))
		if response.Code != 401 {
			t.Fatalf("attempt %d status = %d, want 401; body=%s", attempt+1, response.Code, response.Body.String())
		}
	}
}

func TestCompanionRequestsUseGlobalRateLimitAndNoStore(t *testing.T) {
	router := testRouter(t)
	for attempt := 1; attempt <= 241; attempt++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/companion/tasks/claims", nil))
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("attempt %d Cache-Control = %q, want no-store", attempt, got)
		}
		want := http.StatusUnauthorized
		if attempt == 241 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d; body=%s", attempt, response.Code, want, response.Body.String())
		}
	}
}

func TestCreateRejectsExplicitEmptyDefaultedFields(t *testing.T) {
	router := testRouter(t)
	base := map[string]any{
		"name": "Station", "baseUrl": "https://station.example", "adapter": "new-api",
		"userId": "42", "accessToken": "station-token", "checkinCron": "15 8 * * *",
		"announcementCron": "*/30 * * * *", "timezone": "Asia/Shanghai",
	}
	for _, field := range []string{"adapter", "checkinCron", "announcementCron", "timezone"} {
		t.Run(field, func(t *testing.T) {
			payload := map[string]any{}
			for key, value := range base {
				payload[key] = value
			}
			payload[field] = ""
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/sites", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+testAdminToken)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 422 || !strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestListLimitMatchesNodeNumberCoercion(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int
		ok   bool
	}{
		{"1e2", 100, true},
		{"1.0", 1, true},
		{"0x10", 16, true},
		{"0b10", 2, true},
		{"0o10", 8, true},
		{"1.5", 0, false},
		{"101", 0, false},
	} {
		got, ok := parseBoundedInteger(test.raw, 1, 100)
		if got != test.want || ok != test.ok {
			t.Errorf("parseBoundedInteger(%q) = %d,%v; want %d,%v", test.raw, got, ok, test.want, test.ok)
		}
	}
}

func TestUUIDErrorMessagesMatchNodeContract(t *testing.T) {
	router := testRouter(t)
	for _, test := range []struct {
		method, target, message string
	}{
		{http.MethodGet, "/api/v1/sites/", "siteId: Invalid UUID"},
		{http.MethodGet, "/api/v1/checkin-runs?siteId=invalid", "siteId: Invalid UUID"},
		{http.MethodPatch, "/api/v1/announcements/invalid", "announcementId: Invalid UUID"},
	} {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			request.Header.Set("Authorization", "Bearer "+testAdminToken)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			var payload struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if response.Code != 422 || payload.Error.Code != "VALIDATION_ERROR" || payload.Error.Message != test.message {
				t.Fatalf("status=%d error=%+v", response.Code, payload.Error)
			}
		})
	}
}

func TestStrictBodyErrorsAndSecurityHeaders(t *testing.T) {
	router := testRouter(t)
	tests := []struct {
		name, contentType, body string
		status                  int
		code                    string
	}{
		{"unknown field", "application/json", `{"unexpected":true}`, 422, "VALIDATION_ERROR"},
		{"case mismatched field", "application/json", `{"NAME":"Station"}`, 422, "VALIDATION_ERROR"},
		{"malformed", "application/json", `{"name":`, 400, "BAD_REQUEST"},
		{"unsupported media", "text/plain", `{}`, 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"invalid json media", "application/jsonp", `{}`, 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"oversized", "application/json", `{"name":"` + strings.Repeat("a", 70*1024) + `"}`, 413, "PAYLOAD_TOO_LARGE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/sites", bytes.NewBufferString(test.body))
			request.Header.Set("Authorization", "Bearer "+testAdminToken)
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("body = %s", response.Body.String())
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Security-Policy") == "" {
				t.Fatalf("missing security headers: %v", response.Header())
			}
		})
	}
}

func TestBodyFieldNamesAreCaseSensitive(t *testing.T) {
	router := testRouter(t)
	for _, test := range []struct {
		method, target, body string
	}{
		{http.MethodPost, "/api/v1/sites", `{"NAME":"Station"}`},
		{http.MethodPatch, "/api/v1/sites/11111111-1111-1111-8111-111111111111", `{"ENABLED":false}`},
		{http.MethodPatch, "/api/v1/announcements/11111111-1111-1111-8111-111111111111", `{"READ":true}`},
	} {
		request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer "+testAdminToken)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 422 || !strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.target, response.Code, response.Body.String())
		}
	}
}

func TestDedicatedRateLimitsDoNotConsumeGlobalBudget(t *testing.T) {
	for _, test := range []struct {
		target, limit string
	}{
		{"/api/v1/sites/11111111-1111-1111-8111-111111111111/checkin-runs", "10"},
		{"/api/v1/sites/11111111-1111-1111-8111-111111111111/announcement-syncs", "20"},
	} {
		t.Run(test.limit, func(t *testing.T) {
			router := testRouter(t)
			request := httptest.NewRequest(http.MethodPost, test.target, nil)
			request.Header.Set("Authorization", "Bearer "+testAdminToken)
			special := httptest.NewRecorder()
			router.ServeHTTP(special, request)
			if got := special.Header().Get("X-RateLimit-Limit"); got != test.limit {
				t.Fatalf("dedicated limit = %q, want %q", got, test.limit)
			}

			global := httptest.NewRecorder()
			router.ServeHTTP(global, httptest.NewRequest(http.MethodGet, "/", nil))
			if got := global.Header().Get("X-RateLimit-Remaining"); got != "239" {
				t.Fatalf("global remaining = %q, want 239", got)
			}
		})
	}
}

func TestStrictListQuery(t *testing.T) {
	router := testRouter(t)
	for _, target := range []string{
		"/api/v1/checkin-runs?limit=",
		"/api/v1/checkin-runs?limit=1&limit=2",
		"/api/v1/checkin-runs?siteId=",
		"/api/v1/checkin-runs?siteId=11111111-1111-1111-1111-111111111111&siteId=22222222-2222-2222-2222-222222222222",
	} {
		request := httptest.NewRequest("GET", target, nil)
		request.Header.Set("Authorization", "Bearer "+testAdminToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 422 || !strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestExplicitNullIsRejectedBeforePatchHandling(t *testing.T) {
	router := testRouter(t)
	request := httptest.NewRequest(
		"PATCH",
		"/api/v1/sites/11111111-1111-1111-8111-111111111111",
		bytes.NewBufferString(`{"enabled":false,"name":null}`),
	)
	request.Header.Set("Authorization", "Bearer "+testAdminToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != 422 || !strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUUIDValidationMatchesNodeContract(t *testing.T) {
	for _, value := range []string{
		"00000000-0000-0000-0000-000000000000",
		"11111111-1111-1111-8111-111111111111",
		"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
	} {
		if !validUUID(value) {
			t.Errorf("validUUID(%q) = false", value)
		}
	}
	for _, value := range []string{
		"11111111111111111111111111111111",
		"{11111111-1111-1111-8111-111111111111}",
		"urn:uuid:11111111-1111-1111-8111-111111111111",
		"11111111-1111-1111-1111-111111111111",
		"11111111-1111-9111-8111-111111111111",
	} {
		if validUUID(value) {
			t.Errorf("validUUID(%q) = true", value)
		}
	}
}

func TestEmbeddedAssetsAndSPAFallback(t *testing.T) {
	router := testRouter(t)
	for _, test := range []struct{ path, contentType, contains string }{{"/assets/app.js", "text/javascript", "console.log"}, {"/sites/example", "text/html", "APIHub"}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
		if response.Code != 200 {
			t.Fatalf("%s status=%d", test.path, response.Code)
		}
		if !strings.Contains(response.Header().Get("Content-Type"), test.contentType) {
			t.Fatalf("%s content type=%q", test.path, response.Header().Get("Content-Type"))
		}
		if !strings.Contains(response.Body.String(), test.contains) {
			t.Fatalf("%s body=%s", test.path, response.Body.String())
		}
	}
}

func TestHelmetCompatibleSecurityAndRateLimitHeaders(t *testing.T) {
	router := testRouter(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))

	want := map[string]string{
		"Cross-Origin-Opener-Policy":        "same-origin",
		"Cross-Origin-Resource-Policy":      "same-origin",
		"Origin-Agent-Cluster":              "?1",
		"Referrer-Policy":                   "no-referrer",
		"Strict-Transport-Security":         "max-age=31536000; includeSubDomains",
		"X-Content-Type-Options":            "nosniff",
		"X-DNS-Prefetch-Control":            "off",
		"X-Download-Options":                "noopen",
		"X-Frame-Options":                   "SAMEORIGIN",
		"X-Permitted-Cross-Domain-Policies": "none",
		"X-XSS-Protection":                  "0",
		"X-RateLimit-Limit":                 "240",
		"X-RateLimit-Remaining":             "239",
	}
	for name, value := range want {
		if got := response.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src-attr 'none'") || !strings.Contains(csp, "form-action 'self'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
}
