package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/ent/announcement"
	"github.com/elykia/apihub/server/ent/announcementsyncrun"
	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/google/uuid"
)

type AnnouncementService struct {
	client   *ent.Client
	registry *adapter.Registry
	logger   *slog.Logger
	mu       sync.Mutex
	active   map[string]bool
}

func NewAnnouncementService(client *ent.Client, registry *adapter.Registry, logger *slog.Logger) *AnnouncementService {
	return &AnnouncementService{client: client, registry: registry, logger: logger, active: map[string]bool{}}
}

func (s *AnnouncementService) Sync(ctx context.Context, siteID, requestID string) (AnnouncementSyncView, error) {
	if !s.acquire(siteID) {
		return AnnouncementSyncView{}, apperror.New(409, apperror.Conflict, "An announcement sync is already running for this site", true)
	}
	defer s.release(siteID)
	site, err := s.client.Site.Get(ctx, siteID)
	if ent.IsNotFound(err) {
		return AnnouncementSyncView{}, apperror.New(404, apperror.NotFound, "Site not found", false)
	}
	if err != nil {
		return AnnouncementSyncView{}, fmt.Errorf("get announcement site: %w", err)
	}
	if !site.Enabled || !site.AnnouncementEnabled {
		return AnnouncementSyncView{}, apperror.New(409, apperror.Conflict, "Announcement sync is disabled for this site", false)
	}
	started, err := s.client.AnnouncementSyncRun.Create().SetID(uuid.NewString()).SetSiteID(siteID).SetStatus(announcementsyncrun.StatusRunning).SetAddedCount(0).SetMessage("").SetStartedAt(time.Now().UTC()).SetRequestID(requestID).Save(ctx)
	if err != nil {
		return AnnouncementSyncView{}, fmt.Errorf("create announcement sync: %w", err)
	}
	item, err := s.registry.Get(domain.AdapterName(site.Adapter))
	if err == nil {
		var contextValue domain.SiteContext
		contextValue, err = s.registry.Context(site)
		if err == nil {
			var result domain.AnnouncementResult
			result, err = item.FetchAnnouncements(ctx, contextValue)
			if err == nil {
				added := 0
				for _, announcementItem := range result.Items {
					inserted, upsertErr := s.upsert(ctx, siteID, announcementItem)
					if upsertErr != nil {
						err = upsertErr
						break
					}
					if inserted {
						added++
					}
				}
				if err == nil {
					status := announcementsyncrun.StatusSuccess
					if len(result.Warnings) > 0 {
						status = announcementsyncrun.StatusPartial
					}
					finalizeCtx, cancel := terminalContext(ctx)
					finished, finishErr := started.Update().Where(announcementsyncrun.StatusEQ(announcementsyncrun.StatusRunning)).SetStatus(status).SetAddedCount(added).SetMessage(strings.Join(result.Warnings, "; ")).SetFinishedAt(time.Now().UTC()).Save(finalizeCtx)
					cancel()
					if finishErr != nil {
						return AnnouncementSyncView{}, fmt.Errorf("finish announcement sync: %w", finishErr)
					}
					return announcementSyncView(finished), nil
				}
			}
		}
	}
	appErr := apperror.As(err)
	s.logger.WarnContext(ctx, "announcement sync failed", "siteId", siteID, "requestId", requestID, "errorCode", appErr.Code, "retryable", appErr.Retryable)
	finalizeCtx, cancel := terminalContext(ctx)
	defer cancel()
	finished, finishErr := started.Update().Where(announcementsyncrun.StatusEQ(announcementsyncrun.StatusRunning)).SetStatus(announcementsyncrun.StatusFailed).SetAddedCount(0).SetMessage(appErr.Message).SetFinishedAt(time.Now().UTC()).Save(finalizeCtx)
	if finishErr != nil {
		return AnnouncementSyncView{}, fmt.Errorf("record failed announcement sync: %w", finishErr)
	}
	return announcementSyncView(finished), nil
}

func (s *AnnouncementService) upsert(ctx context.Context, siteID string, item domain.AnnouncementItem) (bool, error) {
	sum := sha256.Sum256([]byte(item.Content))
	fingerprint := hex.EncodeToString(sum[:])
	for attempt := 0; attempt < 3; attempt++ {
		existing, err := s.client.Announcement.Query().Where(announcement.SiteIDEQ(siteID), announcement.FingerprintEQ(fingerprint)).Only(ctx)
		if err == nil {
			update := existing.Update().SetLastSeenAt(time.Now().UTC()).SetContent(item.Content)
			if item.Source == "status" || existing.Source != "status" {
				update.SetSource(announcement.Source(item.Source)).SetKind(item.Kind).SetNillableExtra(item.Extra).SetNillablePublishedAt(item.PublishedAt)
				if item.Extra == nil {
					update.ClearExtra()
				}
				if item.PublishedAt == nil {
					update.ClearPublishedAt()
				}
			}
			if _, err := update.Save(ctx); err != nil {
				return false, fmt.Errorf("update announcement: %w", err)
			}
			return false, nil
		}
		if !ent.IsNotFound(err) {
			return false, fmt.Errorf("find announcement: %w", err)
		}
		now := time.Now().UTC()
		create := s.client.Announcement.Create().SetID(uuid.NewString()).SetSiteID(siteID).SetSource(announcement.Source(item.Source)).SetFingerprint(fingerprint).SetContent(item.Content).SetKind(item.Kind).SetNillableExtra(item.Extra).SetNillablePublishedAt(item.PublishedAt).SetFirstSeenAt(now).SetLastSeenAt(now)
		if _, err := create.Save(ctx); err == nil {
			return true, nil
		} else if !ent.IsConstraintError(err) {
			return false, fmt.Errorf("insert announcement: %w", err)
		}
	}
	return false, fmt.Errorf("upsert announcement: concurrent insert did not become visible")
}
func (s *AnnouncementService) List(ctx context.Context, siteID *string, limit int) ([]AnnouncementView, error) {
	query := s.client.Announcement.Query().WithSite().Order(func(selector *entsql.Selector) {
		published := selector.C(announcement.FieldPublishedAt)
		firstSeen := selector.C(announcement.FieldFirstSeenAt)
		selector.OrderExpr(entsql.DescExpr(entsql.Expr("COALESCE(" + published + ", " + firstSeen + ")")))
		selector.OrderBy(entsql.Desc(selector.C(announcement.FieldID)))
	}).Limit(limit)
	if siteID != nil {
		query.Where(announcement.SiteIDEQ(*siteID))
	}
	items, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	result := make([]AnnouncementView, 0, len(items))
	for _, item := range items {
		site, err := item.Edges.SiteOrErr()
		if err != nil {
			return nil, fmt.Errorf("load announcement site: %w", err)
		}
		result = append(result, announcementView(item, site.Name))
	}
	return result, nil
}
func (s *AnnouncementService) SetRead(ctx context.Context, id string, read bool) (AnnouncementView, error) {
	item, err := s.client.Announcement.Get(ctx, id)
	if ent.IsNotFound(err) {
		return AnnouncementView{}, apperror.New(404, apperror.NotFound, "Announcement not found", false)
	}
	if err != nil {
		return AnnouncementView{}, fmt.Errorf("get announcement: %w", err)
	}
	update := item.Update()
	if read {
		update.SetReadAt(time.Now().UTC())
	} else {
		update.ClearReadAt()
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return AnnouncementView{}, fmt.Errorf("update announcement read state: %w", err)
	}
	return announcementView(updated, ""), nil
}
func (s *AnnouncementService) acquire(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] {
		return false
	}
	s.active[id] = true
	return true
}
func (s *AnnouncementService) release(id string) { s.mu.Lock(); delete(s.active, id); s.mu.Unlock() }
func announcementView(item *ent.Announcement, siteName string) AnnouncementView {
	return AnnouncementView{ID: item.ID, SiteID: item.SiteID, SiteName: siteName, Source: string(item.Source), Fingerprint: item.Fingerprint, Content: item.Content, Kind: item.Kind, Extra: item.Extra, PublishedAt: nullableISOTime(item.PublishedAt), FirstSeenAt: isoTime(item.FirstSeenAt), LastSeenAt: isoTime(item.LastSeenAt), ReadAt: nullableISOTime(item.ReadAt)}
}
func announcementSyncView(item *ent.AnnouncementSyncRun) AnnouncementSyncView {
	return AnnouncementSyncView{ID: item.ID, SiteID: item.SiteID, Status: string(item.Status), AddedCount: item.AddedCount, Message: item.Message, StartedAt: isoTime(item.StartedAt), FinishedAt: nullableISOTime(item.FinishedAt), RequestID: item.RequestID}
}
