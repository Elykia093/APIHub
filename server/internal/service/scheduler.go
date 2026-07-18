package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/elykia/apihub/server/ent"
	entsite "github.com/elykia/apihub/server/ent/site"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	client        *ent.Client
	checkins      *CheckinService
	announcements *AnnouncementService
	logger        *slog.Logger
	root          context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	cron          *cron.Cron
	loadSites     func(context.Context) ([]*ent.Site, error)
	reloadTimeout time.Duration
}

const defaultSchedulerReloadTimeout = 10 * time.Second

type cronLogger struct{ logger *slog.Logger }

func (l cronLogger) Info(message string, keysAndValues ...any) {
	l.logger.Info(message, keysAndValues...)
}
func (l cronLogger) Error(err error, message string, keysAndValues ...any) {
	keysAndValues = append(keysAndValues, "error", err)
	l.logger.Error(message, keysAndValues...)
}

func NewScheduler(parent context.Context, client *ent.Client, checkins *CheckinService, announcements *AnnouncementService, logger *slog.Logger) *Scheduler {
	root, cancel := context.WithCancel(parent)
	return &Scheduler{
		client: client, checkins: checkins, announcements: announcements, logger: logger, root: root, cancel: cancel,
		loadSites: func(ctx context.Context) ([]*ent.Site, error) {
			return client.Site.Query().Where(entsite.EnabledEQ(true)).All(ctx)
		},
		reloadTimeout: defaultSchedulerReloadTimeout,
	}
}
func (s *Scheduler) Reload(context.Context) error {
	timeout := s.reloadTimeout
	if timeout <= 0 {
		timeout = defaultSchedulerReloadTimeout
	}
	reloadCtx, cancel := context.WithTimeout(s.root, timeout)
	defer cancel()
	sites, err := s.loadSites(reloadCtx)
	if err != nil {
		return fmt.Errorf("load scheduler sites: %w", err)
	}
	cronLog := cronLogger{logger: s.logger}
	runner := cron.New(
		cron.WithParser(scheduleParser),
		cron.WithChain(cron.Recover(cronLog), cron.SkipIfStillRunning(cronLog)),
	)
	for _, site := range sites {
		site := site
		canonicalTimezone, _, err := resolveIANATimezone(site.Timezone)
		if err != nil {
			return fmt.Errorf("load timezone %s: %w", site.Timezone, err)
		}
		if site.CheckinEnabled {
			spec := "CRON_TZ=" + canonicalTimezone + " " + normalizeScheduleExpression(site.CheckinCron)
			if _, err := runner.AddFunc(spec, func() {
				requestID := "scheduler:" + uuid.NewString()
				if _, err := s.checkins.Run(s.root, site.ID, requestID, time.Now()); err != nil {
					s.logger.ErrorContext(s.root, "scheduled check-in crashed", "siteId", site.ID, "requestId", requestID, "error", err)
				}
			}); err != nil {
				return fmt.Errorf("register check-in schedule: %w", err)
			}
		}
		if site.AnnouncementEnabled {
			spec := "CRON_TZ=" + canonicalTimezone + " " + normalizeScheduleExpression(site.AnnouncementCron)
			if _, err := runner.AddFunc(spec, func() {
				requestID := "scheduler:" + uuid.NewString()
				if _, err := s.announcements.Sync(s.root, site.ID, requestID); err != nil {
					s.logger.ErrorContext(s.root, "scheduled announcement sync crashed", "siteId", site.ID, "requestId", requestID, "error", err)
				}
			}); err != nil {
				return fmt.Errorf("register announcement schedule: %w", err)
			}
		}
	}
	s.mu.Lock()
	if err := s.root.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	previous := s.cron
	runner.Start()
	s.cron = runner
	s.mu.Unlock()
	if previous != nil {
		previous.Stop()
	}
	return nil
}
func (s *Scheduler) Stop(ctx context.Context) error {
	s.cancel()
	s.mu.Lock()
	runner := s.cron
	s.cron = nil
	s.mu.Unlock()
	if runner == nil {
		return nil
	}
	stopped := runner.Stop()
	select {
	case <-stopped.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
