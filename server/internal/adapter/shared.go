package adapter

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

var alreadyPattern = regexp.MustCompile(`(?i)already\s*(checked|check(?:ed)?\s*in)|今日已签到|已经签到|重复签到|已签过`)
var manualPattern = regexp.MustCompile(`(?i)turnstile|captcha|验证码|人机验证|二次验证|manual`)
var bearerPattern = regexp.MustCompile(`(?i)^Bearer\s+`)

func record(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}
func has(value map[string]any, key string) bool { _, ok := value[key]; return ok }
func hasAny(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if has(value, key) {
			return true
		}
	}
	return false
}
func stringValue(value any) string { text, _ := value.(string); return strings.TrimSpace(text) }
func boolValue(value any) bool     { result, _ := value.(bool); return result }

func responseMessage(response netclient.Response) string {
	if payload, ok := record(response.JSON); ok {
		for _, key := range []string{"message", "msg", "detail", "data"} {
			if message := stringValue(payload[key]); message != "" {
				return truncate(message, 300)
			}
		}
	}
	if text := strings.TrimSpace(response.Text); text != "" {
		return truncate(text, 300)
	}
	return fmt.Sprintf("HTTP %d", response.Status)
}

func truncate(value string, size int) string {
	chars := []rune(value)
	if len(chars) <= size {
		return value
	}
	return string(chars[:size])
}

func bearerHeaders(token string) map[string]string {
	if !bearerPattern.MatchString(token) {
		token = "Bearer " + token
	}
	return map[string]string{"Accept": "application/json", "Content-Type": "application/json", "Authorization": token}
}

func parseGenericCheckin(response netclient.Response) (domain.CheckinResult, error) {
	payload, _ := record(response.JSON)
	if payload == nil {
		payload = map[string]any{}
	}
	data, _ := record(payload["data"])
	if data == nil {
		data = map[string]any{}
	}
	message := responseMessage(response)
	already := boolValue(payload["already_checked_in"]) || boolValue(data["already_checked_in"]) || boolValue(data["checked_in"]) || alreadyPattern.MatchString(message)
	if already {
		return domain.CheckinResult{Status: "already_checked", Message: message}, nil
	}
	if manualPattern.MatchString(message) {
		return domain.CheckinResult{Status: "manual_required", Message: message}, nil
	}
	success := response.OK && (boolValue(payload["success"]) || stringValue(payload["status"]) == "success" || numberEquals(payload["ret"], 1) || numberEquals(payload["code"], 0) || boolValue(payload["ok"]) || boolValue(data["success"]))
	if !success {
		return domain.CheckinResult{}, apperror.New(502, apperror.UpstreamRejected, "Check-in was rejected: "+message, response.Status >= 500)
	}
	var reward *int64
	for _, value := range []any{data["reward"], data["quota_awarded"], payload["reward"]} {
		if converted, ok := safeInt64(value); ok {
			reward = &converted
			break
		}
	}
	if message == "" {
		message = "Check-in succeeded"
	}
	return domain.CheckinResult{Status: "success", RewardValue: reward, Message: message}, nil
}

func numberEquals(value any, expected int64) bool {
	result, ok := safeInt64(value)
	return ok && result == expected
}
func safeInt64(value any) (int64, bool) {
	number, ok := value.(float64)
	const maxSafeInteger = float64(1<<53 - 1)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < -maxSafeInteger || number > maxSafeInteger {
		return 0, false
	}
	return int64(number), true
}

func normalizePublishedAt(value any) *time.Time {
	if number, ok := value.(float64); ok {
		return publishedAtFromNumber(number)
	}
	text := stringValue(value)
	if text == "" {
		return nil
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return publishedAtFromNumber(number)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z, time.RFC850} {
		if parsed, err := time.Parse(layout, text); err == nil {
			normalized := parsed.UTC()
			return &normalized
		}
	}
	if parsed, err := time.Parse("2006-01-02", text); err == nil {
		normalized := parsed.UTC()
		return &normalized
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006/01/02 15:04:05", "2006/01/02"} {
		if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			normalized := parsed.UTC()
			return &normalized
		}
	}
	return nil
}

func publishedAtFromNumber(number float64) *time.Time {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	if number <= 10_000_000_000 {
		number *= 1000
	}
	const maxJavaScriptDate = 8.64e15
	if number < -maxJavaScriptDate || number > maxJavaScriptDate {
		return nil
	}
	parsed := time.UnixMilli(int64(number)).UTC()
	return &parsed
}

func unsupported(displayName, operation string) error {
	return apperror.New(409, apperror.Conflict, fmt.Sprintf("%s does not support %s", displayName, operation), false)
}
