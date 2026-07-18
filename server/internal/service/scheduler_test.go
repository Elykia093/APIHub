package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/elykia/apihub/server/ent"
	"github.com/robfig/cron/v3"
)

func TestSchedulerReloadFailureKeepsExistingRunner(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	old := cron.New()
	old.Start()
	scheduler := &Scheduler{
		root: root, cancel: cancelRoot, cron: old,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadSites: func(context.Context) ([]*ent.Site, error) {
			return nil, errors.New("database unavailable")
		},
		reloadTimeout: time.Second,
	}
	if err := scheduler.Reload(context.Background()); err == nil {
		t.Fatal("Reload() succeeded")
	}
	if scheduler.cron != old {
		t.Fatal("failed reload replaced the existing runner")
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := scheduler.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerReloadUsesBoundedInternalContext(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	var sawDeadline bool
	scheduler := &Scheduler{
		root: root, cancel: cancelRoot,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadSites: func(ctx context.Context) ([]*ent.Site, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			_, sawDeadline = ctx.Deadline()
			return nil, nil
		},
		reloadTimeout: time.Second,
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	if err := scheduler.Reload(requestCtx); err != nil {
		t.Fatalf("Reload() inherited request cancellation: %v", err)
	}
	if !sawDeadline || scheduler.cron == nil {
		t.Fatalf("bounded context=%v runner=%v", sawDeadline, scheduler.cron)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := scheduler.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerReloadAcceptsLegacyCaseInsensitiveTimezone(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	scheduler := &Scheduler{
		root: root, cancel: cancelRoot,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		loadSites: func(context.Context) ([]*ent.Site, error) {
			return []*ent.Site{{
				ID:               "legacy-site",
				Timezone:         "asia/shanghai",
				CheckinEnabled:   true,
				CheckinCron:      "15 8 * * *",
				AnnouncementCron: "*/30 * * * *",
			}}, nil
		},
		reloadTimeout: time.Second,
	}
	if err := scheduler.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() rejected legacy timezone: %v", err)
	}
	if scheduler.cron == nil {
		t.Fatal("Reload() did not install a runner")
	}
	if entries := len(scheduler.cron.Entries()); entries != 1 {
		t.Fatalf("runner entries = %d, want 1", entries)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := scheduler.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}
