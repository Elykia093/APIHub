package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/ent/checkinrun"
	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/google/uuid"
)

type CheckinService struct {
	client   *ent.Client
	registry *adapter.Registry
	logger   *slog.Logger
	mu       sync.Mutex
	active   map[string]bool
}

func NewCheckinService(client *ent.Client, registry *adapter.Registry, logger *slog.Logger) *CheckinService {
	return &CheckinService{client: client, registry: registry, logger: logger, active: map[string]bool{}}
}

func (s *CheckinService) Run(ctx context.Context, siteID, requestID string, now time.Time) (CheckinView, error) {
	if !s.acquire(siteID) {
		return CheckinView{}, apperror.New(409, apperror.Conflict, "A check-in is already running for this site", true)
	}
	defer s.release(siteID)
	site, err := s.client.Site.Get(ctx, siteID)
	if ent.IsNotFound(err) {
		return CheckinView{}, apperror.New(404, apperror.NotFound, "Site not found", false)
	}
	if err != nil {
		return CheckinView{}, fmt.Errorf("get check-in site: %w", err)
	}
	if !site.Enabled || !site.CheckinEnabled {
		return CheckinView{}, apperror.New(409, apperror.Conflict, "Check-in is disabled for this site", false)
	}
	dateText, err := localDateFor(now, site.Timezone)
	if err != nil {
		return CheckinView{}, err
	}
	localDate, _ := time.Parse("2006-01-02", dateText)
	existing, err := s.client.CheckinRun.Query().Where(checkinrun.SiteIDEQ(siteID), checkinrun.LocalDateEQ(localDate)).Only(ctx)
	if err == nil && terminalStatus(string(existing.Status)) {
		return checkinView(existing, site.Name), nil
	}
	if err == nil && existing.Status == checkinrun.StatusRunning {
		return CheckinView{}, apperror.New(409, apperror.Conflict, "A check-in is already running for this site", true)
	}
	if err != nil && !ent.IsNotFound(err) {
		return CheckinView{}, fmt.Errorf("find daily check-in: %w", err)
	}
	var run *ent.CheckinRun
	if existing != nil {
		run, err = existing.Update().Where(checkinrun.StatusIn(checkinrun.StatusFailed, checkinrun.StatusSkipped)).SetStatus(checkinrun.StatusRunning).ClearRewardValue().SetMessage("").ClearErrorCode().AddAttemptCount(1).SetStartedAt(time.Now().UTC()).ClearFinishedAt().SetRequestID(requestID).Save(ctx)
		if ent.IsNotFound(err) {
			return CheckinView{}, apperror.New(409, apperror.Conflict, "A check-in is already running for this site", true)
		}
	} else {
		run, err = s.client.CheckinRun.Create().SetID(uuid.NewString()).SetSiteID(siteID).SetLocalDate(localDate).SetStatus(checkinrun.StatusRunning).SetMessage("").SetAttemptCount(1).SetStartedAt(time.Now().UTC()).SetRequestID(requestID).Save(ctx)
		if ent.IsConstraintError(err) {
			current, queryErr := s.client.CheckinRun.Query().Where(checkinrun.SiteIDEQ(siteID), checkinrun.LocalDateEQ(localDate)).Only(ctx)
			if queryErr == nil && terminalStatus(string(current.Status)) {
				return checkinView(current, site.Name), nil
			}
			return CheckinView{}, apperror.New(409, apperror.Conflict, "A check-in is already running for this site", true)
		}
	}
	if err != nil {
		return CheckinView{}, fmt.Errorf("begin check-in: %w", err)
	}
	item, err := s.registry.Get(domain.AdapterName(site.Adapter))
	if err != nil {
		return s.finishFailure(ctx, run, site, requestID, err)
	}
	contextValue, err := s.registry.Context(site)
	if err != nil {
		return s.finishFailure(ctx, run, site, requestID, err)
	}
	result, err := item.CheckIn(ctx, contextValue, dateText)
	if err != nil {
		return s.finishFailure(ctx, run, site, requestID, err)
	}
	update := run.Update().Where(checkinrun.StatusEQ(checkinrun.StatusRunning)).SetStatus(checkinrun.Status(result.Status)).SetMessage(result.Message).SetFinishedAt(time.Now().UTC()).ClearErrorCode()
	if result.RewardValue != nil {
		update.SetRewardValue(*result.RewardValue)
	} else {
		update.ClearRewardValue()
	}
	if result.Status == "manual_required" {
		update.SetErrorCode(apperror.ManualActionRequired)
	}
	finalizeCtx, cancel := terminalContext(ctx)
	defer cancel()
	finished, err := update.Save(finalizeCtx)
	if err != nil {
		return CheckinView{}, fmt.Errorf("finish check-in: %w", err)
	}
	mode := 0
	if result.Status == "manual_required" {
		mode = 1
	}
	if err := s.setFailureCount(finalizeCtx, site, mode); err != nil {
		return CheckinView{}, err
	}
	return checkinView(finished, site.Name), nil
}

func (s *CheckinService) finishFailure(ctx context.Context, run *ent.CheckinRun, site *ent.Site, requestID string, cause error) (CheckinView, error) {
	appErr := apperror.As(cause)
	s.logger.WarnContext(ctx, "check-in failed", "siteId", site.ID, "requestId", requestID, "errorCode", appErr.Code, "retryable", appErr.Retryable)
	status := checkinrun.StatusFailed
	if appErr.Code == apperror.ManualActionRequired {
		status = checkinrun.StatusManualRequired
	}
	finalizeCtx, cancel := terminalContext(ctx)
	defer cancel()
	finished, err := run.Update().Where(checkinrun.StatusEQ(checkinrun.StatusRunning)).SetStatus(status).SetMessage(appErr.Message).SetErrorCode(appErr.Code).SetFinishedAt(time.Now().UTC()).Save(finalizeCtx)
	if err != nil {
		return CheckinView{}, fmt.Errorf("record failed check-in: %w", err)
	}
	if err := s.setFailureCount(finalizeCtx, site, 1); err != nil {
		return CheckinView{}, err
	}
	return checkinView(finished, site.Name), nil
}
func (s *CheckinService) setFailureCount(ctx context.Context, site *ent.Site, increment int) error {
	update := site.Update().SetUpdatedAt(time.Now().UTC())
	if increment == 0 {
		update.SetConsecutiveFailures(0)
	} else {
		update.AddConsecutiveFailures(1)
	}
	if _, err := update.Save(ctx); err != nil {
		return fmt.Errorf("update site failure count: %w", err)
	}
	return nil
}
func (s *CheckinService) List(ctx context.Context, siteID *string, limit int) ([]CheckinView, error) {
	query := s.client.CheckinRun.Query().WithSite().Order(ent.Desc(checkinrun.FieldStartedAt), ent.Desc(checkinrun.FieldID)).Limit(limit)
	if siteID != nil {
		query.Where(checkinrun.SiteIDEQ(*siteID))
	}
	items, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list check-ins: %w", err)
	}
	result := make([]CheckinView, 0, len(items))
	for _, item := range items {
		site, err := item.Edges.SiteOrErr()
		if err != nil {
			return nil, fmt.Errorf("load check-in site: %w", err)
		}
		result = append(result, checkinView(item, site.Name))
	}
	return result, nil
}
func (s *CheckinService) acquire(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return false
	}
	s.active[id] = true
	return true
}
func (s *CheckinService) release(id string) { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }
func terminalStatus(status string) bool {
	return status == "success" || status == "already_checked" || status == "manual_required"
}
func checkinView(item *ent.CheckinRun, siteName string) CheckinView {
	return CheckinView{ID: item.ID, SiteID: item.SiteID, SiteName: siteName, LocalDate: item.LocalDate.Format("2006-01-02"), Status: string(item.Status), RewardValue: item.RewardValue, Message: item.Message, ErrorCode: item.ErrorCode, AttemptCount: item.AttemptCount, StartedAt: isoTime(item.StartedAt), FinishedAt: nullableISOTime(item.FinishedAt), RequestID: item.RequestID}
}
