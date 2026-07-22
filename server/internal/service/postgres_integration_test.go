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
	t.Run("empty database installs v1 through v4", func(t *testing.T) {
		db, _ := integrationSchema(t)
		if err := migrate.Run(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 4 {
			t.Fatalf("migration count = %d, want 4", count)
		}
		for _, table := range []string{"companion_pairing_codes", "companion_devices", "browser_tasks"} {
			var relation sql.NullString
			if err := db.QueryRow("SELECT to_regclass($1)", table).Scan(&relation); err != nil {
				t.Fatal(err)
			}
			if !relation.Valid {
				t.Fatalf("migration v3 table %s is missing", table)
			}
		}
	})

	t.Run("v1 database upgrades through v4", func(t *testing.T) {
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
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 4 {
			t.Fatalf("migration count after v1 upgrade = %d, err=%v; want 4", count, err)
		}
		if _, err := db.Exec(`INSERT INTO sites(id, name, base_url, adapter, user_id, access_token_ciphertext, checkin_cron, announcement_cron, timezone, created_at, updated_at) VALUES($1, 'sub2', 'https://sub2.example', 'sub2api', '', 'cipher', '15 8 * * *', '*/30 * * * *', 'UTC', NOW(), NOW())`, uuid.NewString()); err != nil {
			t.Fatalf("v2 adapter constraint is not active: %v", err)
		}
		var relation sql.NullString
		if err := db.QueryRow("SELECT to_regclass('browser_tasks')").Scan(&relation); err != nil || !relation.Valid {
			t.Fatalf("v3 browser_tasks table is missing after v1 upgrade: %v", err)
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

func TestPostgresCompanionConcurrentClaims(t *testing.T) {
	db, _ := integrationSchema(t)
	if err := migrate.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	siteIDs := []string{uuid.NewString(), uuid.NewString()}
	for index, siteID := range siteIDs {
		if _, err := db.Exec(`INSERT INTO sites(id, name, base_url, adapter, user_id, access_token_ciphertext, checkin_cron, announcement_cron, timezone, created_at, updated_at) VALUES($1, $2, $3, 'new-api', '1', 'cipher', '15 8 * * *', '*/30 * * * *', 'UTC', $4, $4)`, siteID, fmt.Sprintf("companion-%d", index+1), fmt.Sprintf("https://companion-%d.example", index+1), now); err != nil {
			t.Fatal(err)
		}
	}
	deviceIDs := []string{uuid.NewString(), uuid.NewString()}
	for index, deviceID := range deviceIDs {
		if _, err := db.Exec(`INSERT INTO companion_devices(id, name, token_hash, created_at) VALUES($1, $2, $3, $4)`, deviceID, fmt.Sprintf("device-%d", index+1), secretHash(fmt.Sprintf("token-%d", index+1)), now); err != nil {
			t.Fatal(err)
		}
	}
	for index := range deviceIDs {
		if _, err := db.Exec(`INSERT INTO browser_tasks(id, site_id, target_url, status, created_at) VALUES($1, $2, $3, 'queued', $4)`, uuid.NewString(), siteIDs[index], fmt.Sprintf("https://companion-%d.example/task", index+1), now.Add(time.Duration(index)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
      CREATE FUNCTION delay_browser_task_lease() RETURNS trigger LANGUAGE plpgsql AS $$
      BEGIN
        IF OLD.status = 'queued' AND NEW.status = 'leased' THEN
          PERFORM pg_sleep(0.2);
        END IF;
        RETURN NEW;
      END
      $$
    `); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
      CREATE TRIGGER delay_browser_task_lease
      BEFORE UPDATE ON browser_tasks
      FOR EACH ROW EXECUTE FUNCTION delay_browser_task_lease()
    `); err != nil {
		t.Fatal(err)
	}

	type claimResult struct {
		claim *ClaimedBrowserTask
		err   error
	}
	service := NewCompanionService(db)
	start := make(chan struct{})
	results := make(chan claimResult, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		go func(id string) {
			<-start
			claim, err := service.Claim(context.Background(), id)
			results <- claimResult{claim: claim, err: err}
		}(deviceID)
	}
	close(start)

	claimedIDs := map[string]bool{}
	for range deviceIDs {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.claim == nil {
				t.Fatal("concurrent claim returned no task while another queued task existed")
			}
			if claimedIDs[result.claim.Task.ID] {
				t.Fatalf("task %s was claimed twice", result.claim.Task.ID)
			}
			claimedIDs[result.claim.Task.ID] = true
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent companion claims timed out")
		}
	}
}

func TestPostgresCompanionSingleActiveTaskPerDevice(t *testing.T) {
	db, _ := integrationSchema(t)
	if err := migrate.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	deviceID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO companion_devices(id, name, token_hash, created_at) VALUES($1, 'single-device', $2, $3)`, deviceID, secretHash("single-device-token"), now); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		siteID := uuid.NewString()
		baseURL := fmt.Sprintf("https://single-device-%d.example", index+1)
		if _, err := db.Exec(`INSERT INTO sites(id, name, base_url, adapter, user_id, access_token_ciphertext, checkin_cron, announcement_cron, timezone, created_at, updated_at) VALUES($1, $2, $3, 'new-api', '1', 'cipher', '15 8 * * *', '*/30 * * * *', 'UTC', $4, $4)`, siteID, fmt.Sprintf("single-device-%d", index+1), baseURL, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO browser_tasks(id, site_id, target_url, status, created_at) VALUES($1, $2, $3, 'queued', $4)`, uuid.NewString(), siteID, baseURL+"/task", now.Add(time.Duration(index)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}

	type claimResult struct {
		claim *ClaimedBrowserTask
		err   error
	}
	service := NewCompanionService(db)
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for range 2 {
		go func() {
			<-start
			claim, err := service.Claim(context.Background(), deviceID)
			results <- claimResult{claim: claim, err: err}
		}()
	}
	close(start)

	claimed, empty := 0, 0
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.claim == nil {
				empty++
			} else {
				claimed++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("same-device companion claims timed out")
		}
	}
	if claimed != 1 || empty != 1 {
		t.Fatalf("same-device claims: claimed=%d empty=%d, want 1 each", claimed, empty)
	}
	var leased, queued int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE status = 'leased' AND assigned_device_id = $1), count(*) FILTER (WHERE status = 'queued') FROM browser_tasks`, deviceID).Scan(&leased, &queued); err != nil {
		t.Fatal(err)
	}
	if leased != 1 || queued != 1 {
		t.Fatalf("task states: leased=%d queued=%d, want 1 each", leased, queued)
	}
}

func TestPostgresCompanionLifecycle(t *testing.T) {
	db, _ := integrationSchema(t)
	if err := migrate.Run(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	siteID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO sites(id, name, base_url, adapter, user_id, access_token_ciphertext, checkin_cron, announcement_cron, timezone, created_at, updated_at) VALUES($1, 'companion-lifecycle', 'https://lifecycle.example', 'new-api', '1', 'cipher', '15 8 * * *', '*/30 * * * *', 'UTC', $2, $2)`, siteID, now); err != nil {
		t.Fatal(err)
	}

	companion := NewCompanionService(db)
	pair := func(name string) (CompanionDevice, string) {
		t.Helper()
		code, _, err := companion.CreatePairingCode(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		device, token, err := companion.Pair(context.Background(), code, name)
		if err != nil {
			t.Fatal(err)
		}
		return device, token
	}

	firstCode, _, err := companion.CreatePairingCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, firstToken, err := companion.Pair(context.Background(), firstCode, "Chrome 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := companion.Pair(context.Background(), firstCode, "Chrome duplicate"); apperror.As(err).Code != apperror.AuthRequired {
		t.Fatalf("reused pairing code error = %v, want %s", err, apperror.AuthRequired)
	}
	expiredCode := "EXPIRED-COMPANION-CODE"
	if _, err := db.Exec(`INSERT INTO companion_pairing_codes(id, code_hash, expires_at, created_at) VALUES($1, $2, $3, $4)`, uuid.NewString(), secretHash(expiredCode), now.Add(-time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := companion.Pair(context.Background(), expiredCode, "Chrome expired"); apperror.As(err).Code != apperror.AuthRequired {
		t.Fatalf("expired pairing code error = %v, want %s", err, apperror.AuthRequired)
	}
	if authenticated, err := companion.Authenticate(context.Background(), firstToken); err != nil || authenticated.ID != first.ID {
		t.Fatalf("authenticate paired device = %+v, err=%v", authenticated, err)
	}

	second, _ := pair("Chrome 2")
	third, _ := pair("Chrome 3")

	task, err := companion.CreateTask(context.Background(), siteID, " HTTPS://LIFECYCLE.EXAMPLE:443/console#fragment ")
	if err != nil {
		t.Fatal(err)
	}
	if task.TargetURL != "https://lifecycle.example/console" {
		t.Fatalf("normalized task target = %q", task.TargetURL)
	}
	if _, err := companion.CreateTask(context.Background(), siteID, "https://lifecycle.example/duplicate"); apperror.As(err).Code != apperror.Conflict {
		t.Fatalf("duplicate active task error = %v, want %s", err, apperror.Conflict)
	}
	if _, err := companion.CreateTask(context.Background(), siteID, "https://other.example/console"); apperror.As(err).Code != apperror.ValidationError {
		t.Fatalf("cross-origin task error = %v, want %s", err, apperror.ValidationError)
	}

	firstClaim, err := companion.Claim(context.Background(), first.ID)
	if err != nil || firstClaim == nil || firstClaim.Task.ID != task.ID {
		t.Fatalf("first claim = %+v, err=%v", firstClaim, err)
	}
	if _, err := companion.Heartbeat(context.Background(), second.ID, task.ID, firstClaim.LeaseToken); apperror.As(err).Code != apperror.Conflict {
		t.Fatalf("cross-device heartbeat error = %v, want %s", err, apperror.Conflict)
	}
	if _, err := companion.Finish(context.Background(), second.ID, task.ID, firstClaim.LeaseToken, "failed", "cross-device", nil); apperror.As(err).Code != apperror.Conflict {
		t.Fatalf("cross-device result error = %v, want %s", err, apperror.Conflict)
	}
	finished, err := companion.Finish(context.Background(), first.ID, task.ID, firstClaim.LeaseToken, "success", "done", stringPtr("12.34"))
	if err != nil || finished.Status != "success" || finished.Balance == nil || *finished.Balance != "12.34" {
		t.Fatalf("finished task = %+v, err=%v", finished, err)
	}
	replayed, err := companion.Finish(context.Background(), first.ID, task.ID, firstClaim.LeaseToken, "failed", "replacement", stringPtr("0"))
	if err != nil || replayed.Status != "success" || replayed.Message != "done" || replayed.Balance == nil || *replayed.Balance != "12.34" {
		t.Fatalf("idempotent replay = %+v, err=%v", replayed, err)
	}

	revokedTask, err := companion.CreateTask(context.Background(), siteID, "https://lifecycle.example/revoked")
	if err != nil {
		t.Fatal(err)
	}
	revokedClaim, err := companion.Claim(context.Background(), first.ID)
	if err != nil || revokedClaim == nil || revokedClaim.Task.ID != revokedTask.ID {
		t.Fatalf("revoked-device claim = %+v, err=%v", revokedClaim, err)
	}
	if err := companion.RevokeDevice(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := companion.Authenticate(context.Background(), firstToken); apperror.As(err).Code != apperror.AuthRequired {
		t.Fatalf("revoked device authentication error = %v, want %s", err, apperror.AuthRequired)
	}
	reclaimed, err := companion.Claim(context.Background(), second.ID)
	if err != nil || reclaimed == nil || reclaimed.Task.ID != revokedTask.ID {
		t.Fatalf("reclaimed revoked task = %+v, err=%v", reclaimed, err)
	}
	if _, err := companion.Finish(context.Background(), second.ID, revokedTask.ID, reclaimed.LeaseToken, "success", "released", nil); err != nil {
		t.Fatal(err)
	}

	expiringTask, err := companion.CreateTask(context.Background(), siteID, "https://lifecycle.example/expiring")
	if err != nil {
		t.Fatal(err)
	}
	expiringClaim, err := companion.Claim(context.Background(), second.ID)
	if err != nil || expiringClaim == nil || expiringClaim.Task.ID != expiringTask.ID {
		t.Fatalf("expiring task claim = %+v, err=%v", expiringClaim, err)
	}
	if _, err := db.Exec("UPDATE browser_tasks SET lease_expires_at = $1 WHERE id = $2", time.Now().UTC().Add(-time.Minute), expiringTask.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err = companion.Claim(context.Background(), third.ID)
	if err != nil || reclaimed == nil || reclaimed.Task.ID != expiringTask.ID || reclaimed.Task.AttemptCount != 2 {
		t.Fatalf("expired task re-claim = %+v, err=%v", reclaimed, err)
	}
	if _, err := companion.Heartbeat(context.Background(), second.ID, expiringTask.ID, expiringClaim.LeaseToken); apperror.As(err).Code != apperror.Conflict {
		t.Fatalf("expired lease heartbeat error = %v, want %s", err, apperror.Conflict)
	}
	if _, err := companion.Finish(context.Background(), third.ID, expiringTask.ID, reclaimed.LeaseToken, "already_checked", "already", nil); err != nil {
		t.Fatal(err)
	}
}

func stringPtr(value string) *string { return &value }

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
