package service

import (
	"strings"
	"testing"

	"github.com/elykia/apihub/server/internal/apperror"
)

func TestSiteValidationMatchesJavaScriptStringLength(t *testing.T) {
	if err := validateSiteStrings(strings.Repeat("站", 80), strings.Repeat("用", 128), "token"); err != nil {
		t.Fatalf("valid Unicode lengths rejected: %v", err)
	}
	if err := validateSiteStrings(strings.Repeat("站", 81), "", "token"); err == nil {
		t.Fatal("81-character name accepted")
	}
	if err := validateSiteStrings(strings.Repeat("😀", 40), "", "token"); err != nil {
		t.Fatalf("80 UTF-16 code units rejected: %v", err)
	}
	if err := validateSiteStrings(strings.Repeat("😀", 41), "", "token"); err == nil {
		t.Fatal("82 UTF-16 code units accepted")
	}
}

func TestScheduleLengthBoundaries(t *testing.T) {
	if err := validateSchedule("15 8 * * *", strings.Repeat("x", 101)); err == nil {
		t.Fatal("overlong timezone accepted")
	}
}

func TestScheduleAcceptsNodeOptionalSecondsAndDescriptors(t *testing.T) {
	for _, expression := range []string{
		"15 8 * * *",
		"0 15 8 * * *",
		"@daily",
		"@DAILY",
		"0 0 * * 7",
		"0 0 * * 5-7/2",
		"0 0 ? january sunday",
	} {
		if err := validateSchedule(expression, "UTC"); err != nil {
			t.Errorf("validateSchedule(%q) error = %v", expression, err)
		}
		if _, err := scheduleParser.Parse("CRON_TZ=UTC " + normalizeScheduleExpression(expression)); err != nil {
			t.Errorf("scheduler parser rejected %q: %v", expression, err)
		}
	}
}

func TestIANATimezoneResolutionIsCaseInsensitive(t *testing.T) {
	for input, want := range map[string]string{
		"asia/shanghai": "Asia/Shanghai",
		"utc":           "UTC",
	} {
		canonical, location, err := resolveIANATimezone(input)
		if err != nil {
			t.Errorf("resolveIANATimezone(%q) error = %v", input, err)
			continue
		}
		if canonical != want || location.String() != want {
			t.Errorf("resolveIANATimezone(%q) = %q, %q; want %q", input, canonical, location, want)
		}
	}
}

func TestTimezoneStoragePreservesTrimmedNodeInput(t *testing.T) {
	got, err := normalizedTimezone("  asia/shanghai  ")
	if err != nil || got != "asia/shanghai" {
		t.Fatalf("normalizedTimezone() = %q, %v; want preserved Node value", got, err)
	}
}

func TestInvalidIANATimezoneKeepsValidationContract(t *testing.T) {
	for _, timezone := range []string{"not/an-iana-timezone", "Local", "local"} {
		err := validateSchedule("15 8 * * *", timezone)
		appErr := apperror.As(err)
		if appErr.Status != 422 || appErr.Code != apperror.ValidationError || appErr.Message != "Invalid IANA timezone" {
			t.Errorf("validateSchedule(%q) error = %+v", timezone, appErr)
		}
	}
}
