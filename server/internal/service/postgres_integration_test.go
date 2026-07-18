//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/elykia/apihub/server/ent"
	"github.com/elykia/apihub/server/ent/announcement"
	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/cryptoutil"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/migrate"
	"github.com/elykia/apihub/server/internal/netclient"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresMigrations(t *testing.T) {
	t.Run("empty database installs v1 and v2", func(t *testing.T) {
		db, _ := integrationSchema(t)
		if err := migrate.Run(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("migration count = %d, want 2", count)
		}
	})

	t.Run("v1 database upgrades to v2", func(t *testing.T) {
		db, _ := integrationSchema(t)
		migrations := migrate.All()
		if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, checksum VARCHAR(64) NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(migrations[0].SQL); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO schema_migrations(version, checksum) VALUES($1, $2)", 1, migrate.Checksum(migrations[0].SQL)); err != nil {
			t.Fatal(err)
		}
		if err := migrate.Run(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sites(id, name, base_url, adapter, user_id, access_token_ciphertext, checkin_cron, announcement_cron, timezone, created_at, updated_at) VALUES($1, 'sub2', 'https://sub2.example', 'sub2api', '', 'cipher', '15 8 * * *', '*/30 * * * *', 'UTC', NOW(), NOW())`, uuid.NewString()); err != nil {
			t.Fatalf("v2 adapter constraint not active: %v", err)
		}
	})

	t.Run("checksum drift blocks startup", func(t *testing.T) {
		db, _ := integrationSchema(t)
		if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, checksum VARCHAR(64) NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO schema_migrations(version, checksum) VALUES(1, 'wrong')"); err != nil {
			t.Fatal(err)
		}
		err := migrate.Run(context.Background(), db)
		if err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("Run error = %v, want checksum failure", err)
		}
	})
}

func TestPostgresServiceCompatibility(t *testing.T) {
	db, dsn := integrationSchema(t)
	if err := migrate.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	fake := &integrationAdapter{failFirstCheckin: true}
	vault := cryptoutil.NewVault("integration-app-secret-with-32-characters")
	registry := adapter.NewRegistry(vault, fake)
	httpClient := netclient.New(netclient.Options{Timeout: time.Second, MaxResponseBytes: 64 * 1024})
	t.Cleanup(httpClient.Close)
	sites := NewSiteService(client, registry, adapter.NewDetector(httpClient), vault, false, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	checkins := NewCheckinService(client, registry, logger)
	announcements := NewAnnouncementService(client, registry, logger)

	created := createIntegrationSiteWithTimezone(t, sites, "beta", "https://beta.example", "asia/shanghai")
	if created.Timezone != "asia/shanghai" {
		t.Fatalf("created timezone = %q, want preserved Node value", created.Timezone)
	}
	lowercaseUTC := "utc"
	created, err := sites.Patch(context.Background(), created.ID, PatchSiteInput{Timezone: &lowercaseUTC})
	if err != nil || created.Timezone != "utc" {
		t.Fatalf("patched timezone = %q, err=%v", created.Timezone, err)
	}
	createIntegrationSite(t, sites, "Alpha", "https://alpha.example")
	createIntegrationSite(t, sites, "Beta", "https://beta-upper.example")
	ordered, err := sites.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{ordered[0].Name, ordered[1].Name, ordered[2].Name}; fmt.Sprint(got) != "[Alpha Beta beta]" {
		t.Fatalf("site order = %v", got)
	}

	disabled := false
	patched, err := sites.Patch(context.Background(), created.ID, PatchSiteInput{Enabled: &disabled})
	if err != nil || patched.Enabled {
		t.Fatalf("PATCH false result = %+v, err=%v", patched, err)
	}
	enabled := true
	if _, err := sites.Patch(context.Background(), created.ID, PatchSiteInput{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	first, err := checkins.Run(context.Background(), created.ID, "request-1", now)
	if err != nil || first.Status != "failed" || first.AttemptCount != 1 {
		t.Fatalf("first check-in = %+v, err=%v", first, err)
	}
	second, err := checkins.Run(context.Background(), created.ID, "request-2", now)
	if err != nil || second.Status != "success" || second.AttemptCount != 2 {
		t.Fatalf("retry check-in = %+v, err=%v", second, err)
	}
	third, err := checkins.Run(context.Background(), created.ID, "request-3", now)
	if err != nil || third.ID != second.ID || fake.checkinCalls != 2 {
		t.Fatalf("idempotent check-in = %+v, calls=%d, err=%v", third, fake.checkinCalls, err)
	}
	cancelStarted := make(chan struct{})
	fake.mu.Lock()
	fake.cancelNextCheckin = cancelStarted
	fake.mu.Unlock()
	canceledCtx, cancelCheckin := context.WithCancel(context.Background())
	type canceledResult struct {
		view CheckinView
		err  error
	}
	canceledDone := make(chan canceledResult, 1)
	go func() {
		view, runErr := checkins.Run(canceledCtx, created.ID, "request-canceled", now.AddDate(0, 0, 1))
		canceledDone <- canceledResult{view: view, err: runErr}
	}()
	select {
	case <-cancelStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled check-in did not reach the adapter")
	}
	cancelCheckin()
	select {
	case result := <-canceledDone:
		if result.err != nil || result.view.Status != "failed" {
			t.Fatalf("canceled check-in = %+v, err=%v", result.view, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled check-in did not finalize")
	}
	var canceledStatus string
	if err := db.QueryRow("SELECT status FROM checkin_runs WHERE site_id = $1 AND local_date = $2", created.ID, now.AddDate(0, 0, 1).Format("2006-01-02")).Scan(&canceledStatus); err != nil || canceledStatus != "failed" {
		t.Fatalf("persisted canceled status = %q, err=%v", canceledStatus, err)
	}

	firstSync, err := announcements.Sync(context.Background(), created.ID, "sync-1")
	if err != nil || firstSync.AddedCount != 1 {
		t.Fatalf("first sync = %+v, err=%v", firstSync, err)
	}
	secondSync, err := announcements.Sync(context.Background(), created.ID, "sync-2")
	if err != nil || secondSync.AddedCount != 0 {
		t.Fatalf("deduplicated sync = %+v, err=%v", secondSync, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM announcements WHERE site_id = $1", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("announcement count = %d, err=%v", count, err)
	}
	cancelAnnouncementStarted := make(chan struct{})
	fake.mu.Lock()
	fake.cancelNextAnnouncement = cancelAnnouncementStarted
	fake.mu.Unlock()
	canceledAnnouncementCtx, cancelAnnouncement := context.WithCancel(context.Background())
	type canceledAnnouncementResult struct {
		view AnnouncementSyncView
		err  error
	}
	canceledAnnouncementDone := make(chan canceledAnnouncementResult, 1)
	go func() {
		view, syncErr := announcements.Sync(canceledAnnouncementCtx, created.ID, "sync-canceled")
		canceledAnnouncementDone <- canceledAnnouncementResult{view: view, err: syncErr}
	}()
	select {
	case <-cancelAnnouncementStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled announcement sync did not reach the adapter")
	}
	cancelAnnouncement()
	select {
	case result := <-canceledAnnouncementDone:
		if result.err != nil || result.view.Status != "failed" || result.view.FinishedAt == nil {
			t.Fatalf("canceled announcement sync = %+v, err=%v", result.view, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled announcement sync did not finalize")
	}
	var canceledAnnouncementStatus string
	var canceledAnnouncementFinishedAt sql.NullTime
	if err := db.QueryRow("SELECT status, finished_at FROM announcement_sync_runs WHERE request_id = $1", "sync-canceled").Scan(&canceledAnnouncementStatus, &canceledAnnouncementFinishedAt); err != nil || canceledAnnouncementStatus != "failed" || !canceledAnnouncementFinishedAt.Valid {
		t.Fatalf("persisted canceled announcement status = %q, finished=%v, err=%v", canceledAnnouncementStatus, canceledAnnouncementFinishedAt.Valid, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM announcement_sync_runs WHERE status = 'running'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("running announcement sync count = %d, err=%v", count, err)
	}

	orderSite := createIntegrationSite(t, sites, "Order", "https://order.example")
	base := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	newAnnouncement(t, client, orderSite.ID, "fallback-newer", nil, base.Add(2*time.Hour))
	published := base.Add(time.Hour)
	newAnnouncement(t, client, orderSite.ID, "published-older", &published, base.Add(3*time.Hour))
	feed, err := announcements.List(context.Background(), &orderSite.ID, 10)
	if err != nil || len(feed) != 2 || feed[0].Content != "fallback-newer" {
		t.Fatalf("announcement order = %+v, err=%v", feed, err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.QueryRow("SELECT COUNT(*) FROM sites").Scan(&count); err != nil || count != 4 {
		t.Fatalf("reopened site count = %d, err=%v", count, err)
	}
}

type integrationAdapter struct {
	mu                     sync.Mutex
	checkinCalls           int
	failFirstCheckin       bool
	cancelNextCheckin      chan struct{}
	cancelNextAnnouncement chan struct{}
}

func (a *integrationAdapter) Descriptor() domain.AdapterDescriptor {
	return domain.AdapterDescriptor{
		Name:        domain.NewAPI,
		DisplayName: "New API",
		Capabilities: domain.Capabilities{
			Checkin: true, Announcements: true, RequiresUserID: true,
		},
	}
}

func (a *integrationAdapter) CheckIn(ctx context.Context, _ domain.SiteContext, _ string) (domain.CheckinResult, error) {
	a.mu.Lock()
	a.checkinCalls++
	cancelSignal := a.cancelNextCheckin
	a.cancelNextCheckin = nil
	fail := a.failFirstCheckin && a.checkinCalls == 1
	a.mu.Unlock()
	if cancelSignal != nil {
		close(cancelSignal)
		<-ctx.Done()
		return domain.CheckinResult{}, ctx.Err()
	}
	if fail {
		return domain.CheckinResult{}, apperror.New(502, apperror.UpstreamRejected, "temporary upstream failure", true)
	}
	return domain.CheckinResult{Status: "success", Message: "checked"}, nil
}

func (a *integrationAdapter) FetchAnnouncements(ctx context.Context, _ domain.SiteContext) (domain.AnnouncementResult, error) {
	a.mu.Lock()
	cancelSignal := a.cancelNextAnnouncement
	a.cancelNextAnnouncement = nil
	a.mu.Unlock()
	if cancelSignal != nil {
		close(cancelSignal)
		<-ctx.Done()
		return domain.AnnouncementResult{}, ctx.Err()
	}
	published := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	return domain.AnnouncementResult{Items: []domain.AnnouncementItem{
		{Source: "notice", Content: "same content", Kind: "notice"},
		{Source: "status", Content: "same content", Kind: "status", PublishedAt: &published},
	}}, nil
}

func createIntegrationSite(t *testing.T, sites *SiteService, name, baseURL string) SiteView {
	return createIntegrationSiteWithTimezone(t, sites, name, baseURL, "UTC")
}

func createIntegrationSiteWithTimezone(t *testing.T, sites *SiteService, name, baseURL, timezone string) SiteView {
	t.Helper()
	checkin, announcements := true, true
	created, err := sites.Create(context.Background(), CreateSiteInput{
		Name: name, BaseURL: baseURL, Adapter: string(domain.NewAPI), UserID: "1", AccessToken: "token",
		Enabled: true, CheckinEnabled: &checkin, AnnouncementEnabled: &announcements,
		CheckinCron: "15 8 * * *", AnnouncementCron: "*/30 * * * *", Timezone: timezone,
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func newAnnouncement(t *testing.T, client *ent.Client, siteID, content string, publishedAt *time.Time, firstSeen time.Time) {
	t.Helper()
	_, err := client.Announcement.Create().
		SetID(uuid.NewString()).
		SetSiteID(siteID).
		SetSource(announcement.SourceStatus).
		SetFingerprint(uuid.NewString()).
		SetContent(content).
		SetKind("default").
		SetNillablePublishedAt(publishedAt).
		SetFirstSeenAt(firstSeen).
		SetLastSeenAt(firstSeen).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func integrationSchema(t *testing.T) (*sql.DB, string) {
	t.Helper()
	raw := os.Getenv("APIHUB_INTEGRATION_DATABASE_URL")
	if raw == "" {
		t.Skip("APIHUB_INTEGRATION_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimPrefix(parsed.Path, "/") != "apihub_test" {
		t.Fatalf("integration database must be named apihub_test")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	dsn := parsed.String()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
		_ = admin.Close()
	})
	return db, dsn
}
