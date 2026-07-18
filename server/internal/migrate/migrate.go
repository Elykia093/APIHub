package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

type Migration struct {
	Version int
	SQL     string
}

var migrations = []Migration{
	{
		Version: 1,
		SQL: `
      CREATE TABLE sites (
        id UUID PRIMARY KEY,
        name VARCHAR(80) NOT NULL,
        base_url TEXT NOT NULL UNIQUE,
        adapter VARCHAR(32) NOT NULL CHECK (adapter IN ('new-api')),
        user_id VARCHAR(128) NOT NULL,
        access_token_ciphertext TEXT NOT NULL,
        enabled BOOLEAN NOT NULL DEFAULT TRUE,
        checkin_enabled BOOLEAN NOT NULL DEFAULT TRUE,
        announcement_enabled BOOLEAN NOT NULL DEFAULT TRUE,
        checkin_cron VARCHAR(100) NOT NULL,
        announcement_cron VARCHAR(100) NOT NULL,
        timezone VARCHAR(100) NOT NULL,
        consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
        created_at TIMESTAMPTZ NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL
      );

      CREATE TABLE checkin_runs (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        local_date DATE NOT NULL,
        status VARCHAR(32) NOT NULL CHECK (status IN ('running', 'success', 'already_checked', 'manual_required', 'failed', 'skipped')),
        reward_value BIGINT,
        message TEXT NOT NULL DEFAULT '',
        error_code VARCHAR(64),
        attempt_count INTEGER NOT NULL DEFAULT 1 CHECK (attempt_count >= 1),
        started_at TIMESTAMPTZ NOT NULL,
        finished_at TIMESTAMPTZ,
        request_id TEXT NOT NULL,
        UNIQUE (site_id, local_date)
      );

      CREATE INDEX checkin_runs_started_idx ON checkin_runs(started_at DESC, id DESC);
      CREATE INDEX checkin_runs_site_started_idx ON checkin_runs(site_id, started_at DESC, id DESC);
      CREATE INDEX checkin_runs_local_date_status_idx ON checkin_runs(local_date, status);

      CREATE TABLE announcement_sync_runs (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        status VARCHAR(32) NOT NULL CHECK (status IN ('running', 'success', 'partial', 'failed')),
        added_count INTEGER NOT NULL DEFAULT 0 CHECK (added_count >= 0),
        message TEXT NOT NULL DEFAULT '',
        started_at TIMESTAMPTZ NOT NULL,
        finished_at TIMESTAMPTZ,
        request_id TEXT NOT NULL
      );

      CREATE INDEX announcement_sync_runs_started_idx ON announcement_sync_runs(started_at DESC, id DESC);

      CREATE TABLE announcements (
        id UUID PRIMARY KEY,
        site_id UUID NOT NULL REFERENCES sites(id) ON DELETE RESTRICT,
        source VARCHAR(16) NOT NULL CHECK (source IN ('status', 'notice')),
        fingerprint TEXT NOT NULL,
        content TEXT NOT NULL,
        kind VARCHAR(32) NOT NULL DEFAULT 'default',
        extra TEXT,
        published_at TIMESTAMPTZ,
        first_seen_at TIMESTAMPTZ NOT NULL,
        last_seen_at TIMESTAMPTZ NOT NULL,
        read_at TIMESTAMPTZ,
        UNIQUE (site_id, fingerprint)
      );

      CREATE INDEX announcements_feed_idx ON announcements((COALESCE(published_at, first_seen_at)) DESC, id DESC);
      CREATE INDEX announcements_site_feed_idx ON announcements(site_id, (COALESCE(published_at, first_seen_at)) DESC, id DESC);
      CREATE INDEX announcements_unread_idx ON announcements(first_seen_at DESC) WHERE read_at IS NULL;
    `,
	},
	{
		Version: 2,
		SQL: `
      ALTER TABLE sites DROP CONSTRAINT IF EXISTS sites_adapter_check;
      -- pg-mem names an unnamed CHECK differently; this is a no-op on PostgreSQL.
      ALTER TABLE sites DROP CONSTRAINT IF EXISTS sites_constraint_1;
      ALTER TABLE sites
        ADD CONSTRAINT sites_adapter_check
        CHECK (adapter IN ('new-api', 'sub2api', 'zen-api'));
    `,
	},
}

func All() []Migration { return append([]Migration(nil), migrations...) }

func Checksum(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func Run(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", 726451238); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        checksum VARCHAR(64) NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
      )`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	applied := map[int]string{}
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan migration state: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration state: %w", err)
	}
	known := map[int]bool{}
	for _, migration := range migrations {
		known[migration.Version] = true
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("database schema version %d is not supported by this application build", version)
		}
	}
	for _, migration := range migrations {
		checksum := Checksum(migration.SQL)
		if current, ok := applied[migration.Version]; ok {
			if current != checksum {
				return fmt.Errorf("database migration %d checksum does not match this application build", migration.Version)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %d: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", migration.Version, checksum); err != nil {
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
