package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/ent/announcement"
	"github.com/elykia/apihub/server/ent/checkinrun"
	entsite "github.com/elykia/apihub/server/ent/site"
	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/cryptoutil"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type SiteService struct {
	client                      *ent.Client
	registry                    *adapter.Registry
	detector                    *adapter.Detector
	vault                       *cryptoutil.Vault
	allowPrivate, allowInsecure bool
}

var scheduleParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)
var dayOfWeekRange = regexp.MustCompile(`^(\d+)-(\d+)(?:/(\d+))?$`)

func NewSiteService(client *ent.Client, registry *adapter.Registry, detector *adapter.Detector, vault *cryptoutil.Vault, allowPrivate, allowInsecure bool) *SiteService {
	return &SiteService{client: client, registry: registry, detector: detector, vault: vault, allowPrivate: allowPrivate, allowInsecure: allowInsecure}
}

func (s *SiteService) List(ctx context.Context) ([]SiteView, error) {
	items, err := s.client.Site.Query().Order(func(selector *entsql.Selector) {
		selector.OrderExpr(entsql.Expr("LOWER(" + selector.C(entsite.FieldName) + ")"))
		selector.OrderBy(selector.C(entsite.FieldName), selector.C(entsite.FieldID))
	}).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	result := make([]SiteView, 0, len(items))
	for _, item := range items {
		view, err := s.view(item)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *SiteService) Get(ctx context.Context, id string) (SiteView, error) {
	item, err := s.getEntity(ctx, id)
	if err != nil {
		return SiteView{}, err
	}
	return s.view(item)
}

func (s *SiteService) Create(ctx context.Context, input CreateSiteInput) (SiteView, error) {
	if err := validateSiteStrings(input.Name, input.UserID, input.AccessToken); err != nil {
		return SiteView{}, err
	}
	baseURL, err := netclient.NormalizeBaseURL(input.BaseURL, s.allowInsecure, s.allowPrivate)
	if err != nil {
		return SiteView{}, err
	}
	name, err := s.resolveAdapter(ctx, input.Adapter, baseURL)
	if err != nil {
		return SiteView{}, err
	}
	descriptor, err := s.descriptor(name)
	if err != nil {
		return SiteView{}, err
	}
	checkinEnabled := descriptor.Capabilities.Checkin
	if input.CheckinEnabled != nil {
		checkinEnabled = *input.CheckinEnabled
	}
	announcementEnabled := descriptor.Capabilities.Announcements
	if input.AnnouncementEnabled != nil {
		announcementEnabled = *input.AnnouncementEnabled
	}
	if err := validateAdapterConfiguration(descriptor, input.UserID, checkinEnabled, announcementEnabled); err != nil {
		return SiteView{}, err
	}
	timezone, err := normalizedTimezone(input.Timezone)
	if err != nil {
		return SiteView{}, err
	}
	if err := validateSchedule(input.CheckinCron, timezone); err != nil {
		return SiteView{}, err
	}
	if err := validateSchedule(input.AnnouncementCron, timezone); err != nil {
		return SiteView{}, err
	}
	ciphertext, err := s.vault.Encrypt(input.AccessToken)
	if err != nil {
		return SiteView{}, fmt.Errorf("encrypt site credential: %w", err)
	}
	now := time.Now().UTC()
	created, err := s.client.Site.Create().SetID(uuid.NewString()).SetName(strings.TrimSpace(input.Name)).SetBaseURL(baseURL).SetAdapter(entsite.Adapter(name)).SetUserID(strings.TrimSpace(input.UserID)).SetAccessTokenCiphertext(ciphertext).SetEnabled(input.Enabled).SetCheckinEnabled(checkinEnabled).SetAnnouncementEnabled(announcementEnabled).SetCheckinCron(strings.TrimSpace(input.CheckinCron)).SetAnnouncementCron(strings.TrimSpace(input.AnnouncementCron)).SetTimezone(timezone).SetConsecutiveFailures(0).SetCreatedAt(now).SetUpdatedAt(now).Save(ctx)
	if err != nil {
		return SiteView{}, normalizeWriteError(err)
	}
	return s.view(created)
}

func (s *SiteService) Patch(ctx context.Context, id string, input PatchSiteInput) (SiteView, error) {
	current, err := s.getEntity(ctx, id)
	if err != nil {
		return SiteView{}, err
	}
	baseURL := current.BaseURL
	if input.BaseURL != nil {
		baseURL, err = netclient.NormalizeBaseURL(*input.BaseURL, s.allowInsecure, s.allowPrivate)
		if err != nil {
			return SiteView{}, err
		}
	}
	adapterName := domain.AdapterName(current.Adapter)
	if input.Adapter != nil {
		adapterName, err = s.resolveAdapter(ctx, *input.Adapter, baseURL)
		if err != nil {
			return SiteView{}, err
		}
	}
	userID := current.UserID
	if input.UserID != nil {
		userID = strings.TrimSpace(*input.UserID)
	}
	checkinEnabled := current.CheckinEnabled
	if input.CheckinEnabled != nil {
		checkinEnabled = *input.CheckinEnabled
	}
	announcementEnabled := current.AnnouncementEnabled
	if input.AnnouncementEnabled != nil {
		announcementEnabled = *input.AnnouncementEnabled
	}
	descriptor, err := s.descriptor(adapterName)
	if err != nil {
		return SiteView{}, err
	}
	if err := validateAdapterConfiguration(descriptor, userID, checkinEnabled, announcementEnabled); err != nil {
		return SiteView{}, err
	}
	checkinCron := current.CheckinCron
	if input.CheckinCron != nil {
		checkinCron = strings.TrimSpace(*input.CheckinCron)
	}
	announcementCron := current.AnnouncementCron
	if input.AnnouncementCron != nil {
		announcementCron = strings.TrimSpace(*input.AnnouncementCron)
	}
	timezone := current.Timezone
	if input.Timezone != nil {
		timezone, err = normalizedTimezone(*input.Timezone)
		if err != nil {
			return SiteView{}, err
		}
	}
	if err := validateSchedule(checkinCron, timezone); err != nil {
		return SiteView{}, err
	}
	if err := validateSchedule(announcementCron, timezone); err != nil {
		return SiteView{}, err
	}
	update := current.Update().SetUpdatedAt(time.Now().UTC())
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || jsStringLength(name) > 80 {
			return SiteView{}, validation("name must contain 1 to 80 characters")
		}
		update.SetName(name)
	}
	if input.BaseURL != nil {
		update.SetBaseURL(baseURL)
	}
	if input.Adapter != nil {
		update.SetAdapter(entsite.Adapter(adapterName))
	}
	if input.UserID != nil {
		if jsStringLength(userID) > 128 {
			return SiteView{}, validation("userId must contain at most 128 characters")
		}
		update.SetUserID(userID)
	}
	if input.AccessToken != nil {
		if jsStringLength(*input.AccessToken) < 1 || jsStringLength(*input.AccessToken) > 4096 {
			return SiteView{}, validation("accessToken must contain 1 to 4096 characters")
		}
		encrypted, err := s.vault.Encrypt(*input.AccessToken)
		if err != nil {
			return SiteView{}, fmt.Errorf("encrypt site credential: %w", err)
		}
		update.SetAccessTokenCiphertext(encrypted)
	}
	if input.Enabled != nil {
		update.SetEnabled(*input.Enabled)
	}
	if input.CheckinEnabled != nil {
		update.SetCheckinEnabled(*input.CheckinEnabled)
	}
	if input.AnnouncementEnabled != nil {
		update.SetAnnouncementEnabled(*input.AnnouncementEnabled)
	}
	if input.CheckinCron != nil {
		update.SetCheckinCron(checkinCron)
	}
	if input.AnnouncementCron != nil {
		update.SetAnnouncementCron(announcementCron)
	}
	if input.Timezone != nil {
		update.SetTimezone(timezone)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return SiteView{}, normalizeWriteError(err)
	}
	return s.view(updated)
}

func (s *SiteService) Summary(ctx context.Context, now time.Time) (Summary, error) {
	var result Summary
	result.Today = map[string]int{}
	var err error
	if result.Sites.Total, err = s.client.Site.Query().Count(ctx); err != nil {
		return result, fmt.Errorf("count sites: %w", err)
	}
	if result.Sites.Enabled, err = s.client.Site.Query().Where(entsite.EnabledEQ(true)).Count(ctx); err != nil {
		return result, fmt.Errorf("count enabled sites: %w", err)
	}
	if result.UnreadAnnouncements, err = s.client.Announcement.Query().Where(announcement.ReadAtIsNil()).Count(ctx); err != nil {
		return result, fmt.Errorf("count unread announcements: %w", err)
	}
	dates := make([]time.Time, 0, 3)
	for offset := -1; offset <= 1; offset++ {
		date := now.UTC().AddDate(0, 0, offset)
		dates = append(dates, time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC))
	}
	runs, err := s.client.CheckinRun.Query().Where(checkinrun.LocalDateIn(dates...)).WithSite().All(ctx)
	if err != nil {
		return result, fmt.Errorf("query today check-ins: %w", err)
	}
	for _, run := range runs {
		linked, err := run.Edges.SiteOrErr()
		if err != nil {
			return result, fmt.Errorf("load check-in site: %w", err)
		}
		localDate, err := localDateFor(now, linked.Timezone)
		if err != nil {
			return result, err
		}
		if run.LocalDate.Format("2006-01-02") == localDate {
			result.Today[string(run.Status)]++
		}
	}
	return result, nil
}

func (s *SiteService) getEntity(ctx context.Context, id string) (*ent.Site, error) {
	item, err := s.client.Site.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, apperror.New(404, apperror.NotFound, "Site not found", false)
	}
	if err != nil {
		return nil, fmt.Errorf("get site: %w", err)
	}
	return item, nil
}
func (s *SiteService) view(item *ent.Site) (SiteView, error) {
	descriptor, err := s.descriptor(domain.AdapterName(item.Adapter))
	if err != nil {
		return SiteView{}, err
	}
	return SiteView{ID: item.ID, Name: item.Name, BaseURL: item.BaseURL, Adapter: string(item.Adapter), UserID: item.UserID, Enabled: item.Enabled, CheckinEnabled: item.CheckinEnabled, AnnouncementEnabled: item.AnnouncementEnabled, CheckinCron: item.CheckinCron, AnnouncementCron: item.AnnouncementCron, Timezone: item.Timezone, CredentialConfigured: item.AccessTokenCiphertext != "", ConsecutiveFailures: item.ConsecutiveFailures, Capabilities: descriptor.Capabilities, CreatedAt: isoTime(item.CreatedAt), UpdatedAt: isoTime(item.UpdatedAt)}, nil
}
func (s *SiteService) descriptor(name domain.AdapterName) (domain.AdapterDescriptor, error) {
	item, err := s.registry.Get(name)
	if err != nil {
		return domain.AdapterDescriptor{}, err
	}
	return item.Descriptor(), nil
}
func (s *SiteService) resolveAdapter(ctx context.Context, selection, baseURL string) (domain.AdapterName, error) {
	name := domain.AdapterName(selection)
	if selection == "auto" {
		return s.detector.Detect(ctx, baseURL)
	}
	if _, err := s.registry.Get(name); err != nil {
		return "", err
	}
	return name, nil
}
func validateSiteStrings(name, userID, token string) error {
	name = strings.TrimSpace(name)
	if jsStringLength(name) < 1 || jsStringLength(name) > 80 {
		return validation("name must contain 1 to 80 characters")
	}
	if jsStringLength(strings.TrimSpace(userID)) > 128 {
		return validation("userId must contain at most 128 characters")
	}
	if jsStringLength(token) < 1 || jsStringLength(token) > 4096 {
		return validation("accessToken must contain 1 to 4096 characters")
	}
	return nil
}
func validateAdapterConfiguration(descriptor domain.AdapterDescriptor, userID string, checkin, announcements bool) error {
	if descriptor.Capabilities.RequiresUserID && strings.TrimSpace(userID) == "" {
		return validation(descriptor.DisplayName + " requires userId")
	}
	if checkin && !descriptor.Capabilities.Checkin {
		return validation(descriptor.DisplayName + " does not support server-side check-in")
	}
	if announcements && !descriptor.Capabilities.Announcements {
		return validation(descriptor.DisplayName + " does not support announcement sync")
	}
	return nil
}
func validateSchedule(expression, timezone string) error {
	if jsStringLength(strings.TrimSpace(expression)) < 1 || jsStringLength(strings.TrimSpace(expression)) > 100 {
		return validation("Invalid cron expression")
	}
	if jsStringLength(strings.TrimSpace(timezone)) < 1 || jsStringLength(strings.TrimSpace(timezone)) > 100 {
		return validation("Invalid IANA timezone")
	}
	if _, err := scheduleParser.Parse(normalizeScheduleExpression(expression)); err != nil {
		return validation("Invalid cron expression")
	}
	if _, _, err := resolveIANATimezone(timezone); err != nil {
		return validation("Invalid IANA timezone")
	}
	return nil
}

func normalizedTimezone(timezone string) (string, error) {
	timezone = strings.TrimSpace(timezone)
	if jsStringLength(timezone) < 1 || jsStringLength(timezone) > 100 {
		return "", validation("Invalid IANA timezone")
	}
	_, _, err := resolveIANATimezone(timezone)
	if err != nil {
		return "", validation("Invalid IANA timezone")
	}
	return timezone, nil
}

func normalizeScheduleExpression(expression string) string {
	trimmed := strings.TrimSpace(expression)
	if strings.HasPrefix(trimmed, "@") {
		return strings.ToLower(trimmed)
	}
	fields := strings.Fields(trimmed)
	if len(fields) != 5 && len(fields) != 6 {
		return trimmed
	}
	domIndex, monthIndex, dowIndex := len(fields)-3, len(fields)-2, len(fields)-1
	if fields[domIndex] == "?" {
		fields[domIndex] = "*"
	}
	if fields[dowIndex] == "?" {
		fields[dowIndex] = "*"
	}
	fields[monthIndex] = replaceCronNames(fields[monthIndex], []string{
		"january", "february", "march", "april", "may", "june",
		"july", "august", "september", "october", "november", "december",
	})
	fields[dowIndex] = replaceCronNames(fields[dowIndex], []string{
		"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
	})
	fields[dowIndex] = normalizeDayOfWeek(fields[dowIndex])
	return strings.Join(fields, " ")
}

func replaceCronNames(field string, names []string) string {
	field = strings.ToLower(field)
	for _, name := range names {
		field = strings.ReplaceAll(field, name, name[:3])
	}
	return field
}

func normalizeDayOfWeek(field string) string {
	var result []string
	for _, token := range strings.Split(field, ",") {
		token = strings.TrimSpace(token)
		if token == "7" {
			result = append(result, "0")
			continue
		}
		if strings.HasPrefix(token, "7/") {
			result = append(result, "0/"+strings.TrimPrefix(token, "7/"))
			continue
		}
		match := dayOfWeekRange.FindStringSubmatch(token)
		if len(match) != 4 {
			result = append(result, token)
			continue
		}
		first, firstErr := strconv.Atoi(match[1])
		last, lastErr := strconv.Atoi(match[2])
		step := 1
		var stepErr error
		if match[3] != "" {
			step, stepErr = strconv.Atoi(match[3])
		}
		if firstErr != nil || lastErr != nil || stepErr != nil || first > 7 || last > 7 || step < 1 {
			result = append(result, token)
			continue
		}
		span := last - first
		if span < 0 {
			span = (span%7 + 7) % 7
		}
		for offset := 0; offset <= span; offset += step {
			value := (first + offset) % 7
			result = append(result, strconv.Itoa(value))
		}
	}
	return strings.Join(result, ",")
}
func jsStringLength(value string) int { return len(utf16.Encode([]rune(value))) }
func validation(message string) error {
	return apperror.New(422, apperror.ValidationError, message, false)
}
func normalizeWriteError(err error) error {
	if ent.IsConstraintError(err) {
		return apperror.Wrap(409, apperror.Conflict, "A resource with the same unique value already exists", false, err)
	}
	return fmt.Errorf("write site: %w", err)
}
func localDateFor(now time.Time, timezone string) (string, error) {
	_, location, err := resolveIANATimezone(timezone)
	if err != nil {
		return "", validation("Invalid IANA timezone")
	}
	return now.In(location).Format("2006-01-02"), nil
}
