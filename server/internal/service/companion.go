package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/google/uuid"
	"golang.org/x/net/idna"
)

type CompanionDevice struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	CreatedAt  ISOTime  `json:"createdAt"`
	LastSeenAt *ISOTime `json:"lastSeenAt"`
	RevokedAt  *ISOTime `json:"revokedAt"`
}

type BrowserTask struct {
	ID                 string   `json:"id"`
	SiteID             string   `json:"siteId"`
	SiteName           string   `json:"siteName,omitempty"`
	TargetURL          string   `json:"targetUrl"`
	Status             string   `json:"status"`
	AssignedDeviceID   *string  `json:"assignedDeviceId"`
	AssignedDeviceName string   `json:"assignedDeviceName,omitempty"`
	LeaseExpiresAt     *ISOTime `json:"leaseExpiresAt"`
	AttemptCount       int      `json:"attemptCount"`
	Message            string   `json:"message"`
	Balance            *string  `json:"balance"`
	CreatedAt          ISOTime  `json:"createdAt"`
	StartedAt          *ISOTime `json:"startedAt"`
	FinishedAt         *ISOTime `json:"finishedAt"`
}

type ClaimedBrowserTask struct {
	Task       BrowserTask
	LeaseToken string
}

func secretHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate companion token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func normalizeCompanionName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len([]rune(name)) > 80 {
		return "", apperror.New(422, apperror.ValidationError, "deviceName must contain 1 to 80 characters", false)
	}
	return name, nil
}

func CreatePairingCode() (string, string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate pairing code: %w", err)
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), time.Now().UTC().Add(5 * time.Minute).Format("2006-01-02T15:04:05.000Z"), nil
}

type CompanionService struct{ db *sql.DB }

func NewCompanionService(db *sql.DB) *CompanionService { return &CompanionService{db: db} }

func (s *CompanionService) CreatePairingCode(ctx context.Context) (string, string, error) {
	code, expires, err := CreatePairingCode()
	if err != nil {
		return "", "", err
	}
	if _, err := s.db.ExecContext(ctx, `
      INSERT INTO companion_pairing_codes (id, code_hash, expires_at, created_at)
      VALUES ($1, $2, $3, $4)
    `, uuid.NewString(), secretHash(code), expires, time.Now().UTC()); err != nil {
		return "", "", fmt.Errorf("create companion pairing code: %w", err)
	}
	return code, expires, nil
}

func (s *CompanionService) Pair(ctx context.Context, code, deviceName string) (CompanionDevice, string, error) {
	name, err := normalizeCompanionName(deviceName)
	if err != nil {
		return CompanionDevice{}, "", err
	}
	token, err := randomToken(32)
	if err != nil {
		return CompanionDevice{}, "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CompanionDevice{}, "", fmt.Errorf("begin companion pairing: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	var codeID string
	if err := tx.QueryRowContext(ctx, `
      UPDATE companion_pairing_codes SET consumed_at = $1
      WHERE code_hash = $2 AND consumed_at IS NULL AND expires_at > $1
      RETURNING id
    `, now, secretHash(strings.TrimSpace(code))).Scan(&codeID); err != nil {
		if err == sql.ErrNoRows {
			return CompanionDevice{}, "", apperror.New(401, apperror.AuthRequired, "Pairing code is invalid or expired", false)
		}
		return CompanionDevice{}, "", fmt.Errorf("consume companion pairing code: %w", err)
	}
	deviceID := uuid.NewString()
	var created, lastSeen time.Time
	if err := tx.QueryRowContext(ctx, `
      INSERT INTO companion_devices (id, name, token_hash, created_at, last_seen_at)
      VALUES ($1, $2, $3, $4, $4)
      RETURNING created_at, last_seen_at
    `, deviceID, name, secretHash(token), now).Scan(&created, &lastSeen); err != nil {
		return CompanionDevice{}, "", fmt.Errorf("create companion device: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CompanionDevice{}, "", fmt.Errorf("commit companion pairing: %w", err)
	}
	return CompanionDevice{ID: deviceID, Name: name, CreatedAt: isoTime(created), LastSeenAt: nullableISOTime(&lastSeen)}, token, nil
}

func (s *CompanionService) Authenticate(ctx context.Context, token string) (CompanionDevice, error) {
	var device CompanionDevice
	var created time.Time
	var lastSeen, revoked sql.NullTime
	err := s.db.QueryRowContext(ctx, `
      SELECT id, name, created_at, last_seen_at, revoked_at
      FROM companion_devices WHERE token_hash = $1 AND revoked_at IS NULL
    `, secretHash(token)).Scan(&device.ID, &device.Name, &created, &lastSeen, &revoked)
	if err == sql.ErrNoRows {
		return CompanionDevice{}, apperror.New(401, apperror.AuthRequired, "Companion device authentication required", false)
	}
	if err != nil {
		return CompanionDevice{}, fmt.Errorf("authenticate companion device: %w", err)
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, "UPDATE companion_devices SET last_seen_at = $1 WHERE id = $2", now, device.ID); err != nil {
		return CompanionDevice{}, fmt.Errorf("touch companion device: %w", err)
	}
	device.CreatedAt = isoTime(created)
	if lastSeen.Valid {
		device.LastSeenAt = nullableISOTime(&lastSeen.Time)
	}
	if revoked.Valid {
		device.RevokedAt = nullableISOTime(&revoked.Time)
	}
	return device, nil
}

func (s *CompanionService) ListDevices(ctx context.Context) (devices []CompanionDevice, resultErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_seen_at, revoked_at FROM companion_devices ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list companion devices: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close companion device rows: %w", err)
		}
	}()
	devices = []CompanionDevice{}
	for rows.Next() {
		var item CompanionDevice
		var created time.Time
		var lastSeen, revoked sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &created, &lastSeen, &revoked); err != nil {
			return nil, fmt.Errorf("scan companion device: %w", err)
		}
		item.CreatedAt = isoTime(created)
		if lastSeen.Valid {
			item.LastSeenAt = nullableISOTime(&lastSeen.Time)
		}
		if revoked.Valid {
			item.RevokedAt = nullableISOTime(&revoked.Time)
		}
		devices = append(devices, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate companion devices: %w", err)
	}
	return devices, nil
}

func (s *CompanionService) RevokeDevice(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin companion device revocation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "UPDATE companion_devices SET revoked_at = COALESCE(revoked_at, $1) WHERE id = $2", time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("revoke companion device: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return apperror.New(404, apperror.NotFound, "Companion device not found", false)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE browser_tasks SET status = 'queued', assigned_device_id = NULL, lease_token_hash = NULL, lease_expires_at = NULL WHERE assigned_device_id = $1 AND status = 'leased'`, id); err != nil {
		return fmt.Errorf("release companion tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit companion device revocation: %w", err)
	}
	return nil
}

func validateBrowserTarget(baseURL, targetURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", apperror.New(422, apperror.ValidationError, "site base URL is invalid", false)
	}
	target, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil {
		return "", apperror.New(422, apperror.ValidationError, "targetUrl must use the same site origin", false)
	}
	target.Scheme = strings.ToLower(target.Scheme)
	base.Scheme = strings.ToLower(base.Scheme)
	if (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || browserOrigin(target) != browserOrigin(base) {
		return "", apperror.New(422, apperror.ValidationError, "targetUrl must use the same site origin", false)
	}
	target.Fragment = ""
	target.Scheme = strings.ToLower(target.Scheme)
	host := strings.ToLower(target.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	} else if ascii, err := idna.Lookup.ToASCII(host); err != nil {
		return "", apperror.New(422, apperror.ValidationError, "targetUrl must use the same site origin", false)
	} else {
		host = ascii
	}
	port := target.Port()
	if (target.Scheme == "https" && port == "443") || (target.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	target.Host = host
	return target.String(), nil
}

func browserOrigin(value *url.URL) string {
	port := value.Port()
	if (value.Scheme == "https" && port == "443") || (value.Scheme == "http" && port == "80") {
		port = ""
	}
	host := strings.ToLower(value.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	} else if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		host = ascii
	}
	if port != "" {
		host += ":" + port
	}
	return strings.ToLower(value.Scheme) + "://" + host
}

func (s *CompanionService) CreateTask(ctx context.Context, siteID, targetURL string) (BrowserTask, error) {
	var baseURL string
	var enabled bool
	if err := s.db.QueryRowContext(ctx, "SELECT base_url, enabled FROM sites WHERE id = $1", siteID).Scan(&baseURL, &enabled); err == sql.ErrNoRows {
		return BrowserTask{}, apperror.New(404, apperror.NotFound, "Site not found", false)
	} else if err != nil {
		return BrowserTask{}, fmt.Errorf("read browser task site: %w", err)
	}
	if !enabled {
		return BrowserTask{}, apperror.New(409, apperror.Conflict, "Site is disabled", false)
	}
	normalized, err := validateBrowserTarget(baseURL, targetURL)
	if err != nil {
		return BrowserTask{}, err
	}
	task, err := scanTaskBare(s.db.QueryRowContext(ctx, `
      INSERT INTO browser_tasks (id, site_id, target_url, status, created_at)
      VALUES ($1, $2, $3, 'queued', $4)
      ON CONFLICT (site_id) WHERE status IN ('queued', 'leased') DO NOTHING
      RETURNING id, site_id, target_url, status, assigned_device_id, lease_expires_at, attempt_count, message, balance, created_at, started_at, finished_at
    `, uuid.NewString(), siteID, normalized, time.Now().UTC()))
	if err == sql.ErrNoRows {
		return BrowserTask{}, apperror.New(409, apperror.Conflict, "A browser task is already active for this site", false)
	}
	if err != nil {
		return BrowserTask{}, fmt.Errorf("create browser task: %w", err)
	}
	return task, nil
}

func (s *CompanionService) ListTasks(ctx context.Context, limit int) (tasks []BrowserTask, resultErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.site_id, s.name, t.target_url, t.status, t.assigned_device_id, d.name, t.lease_expires_at, t.attempt_count, t.message, t.balance, t.created_at, t.started_at, t.finished_at FROM browser_tasks t JOIN sites s ON s.id = t.site_id LEFT JOIN companion_devices d ON d.id = t.assigned_device_id ORDER BY t.created_at DESC, t.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list browser tasks: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close browser task rows: %w", err)
		}
	}()
	tasks = []BrowserTask{}
	for rows.Next() {
		item, err := scanTaskJoined(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate browser tasks: %w", err)
	}
	return tasks, nil
}

func (s *CompanionService) Claim(ctx context.Context, deviceID string) (*ClaimedBrowserTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin browser task claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	var activeDevice string
	if err := tx.QueryRowContext(ctx, `UPDATE companion_devices SET last_seen_at = $2 WHERE id = $1 AND revoked_at IS NULL RETURNING id`, deviceID, now).Scan(&activeDevice); err == sql.ErrNoRows {
		return nil, apperror.New(401, apperror.AuthRequired, "Companion device authentication required", false)
	} else if err != nil {
		return nil, fmt.Errorf("lock companion device for claim: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE browser_tasks SET status = 'queued', assigned_device_id = NULL, lease_token_hash = NULL, lease_expires_at = NULL WHERE status = 'leased' AND lease_expires_at < $1`, now); err != nil {
		return nil, fmt.Errorf("release expired browser task: %w", err)
	}
	leaseToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(2 * time.Minute)
	var taskID string
	if err := tx.QueryRowContext(ctx, `
      WITH next_task AS (
        SELECT id
        FROM browser_tasks
        WHERE status = 'queued'
          AND NOT EXISTS (
            SELECT 1
            FROM browser_tasks active
            WHERE active.assigned_device_id = $1 AND active.status = 'leased'
          )
        ORDER BY created_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT 1
      )
      UPDATE browser_tasks AS task
      SET status = 'leased', assigned_device_id = $1, lease_token_hash = $2,
          lease_expires_at = $3, attempt_count = attempt_count + 1,
          started_at = COALESCE(started_at, $4)
      FROM next_task
      WHERE task.id = next_task.id
      RETURNING task.id
    `, deviceID, secretHash(leaseToken), leaseUntil, now).Scan(&taskID); err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty browser task claim: %w", err)
		}
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("select browser task: %w", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT t.id, t.site_id, s.name, t.target_url, t.status, t.assigned_device_id, d.name, t.lease_expires_at, t.attempt_count, t.message, t.balance, t.created_at, t.started_at, t.finished_at FROM browser_tasks t JOIN sites s ON s.id = t.site_id LEFT JOIN companion_devices d ON d.id = t.assigned_device_id WHERE t.id = $1`, taskID)
	task, err := scanTaskJoined(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit browser task claim: %w", err)
	}
	return &ClaimedBrowserTask{Task: task, LeaseToken: leaseToken}, nil
}

func (s *CompanionService) Heartbeat(ctx context.Context, deviceID, taskID, leaseToken string) (BrowserTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BrowserTask{}, fmt.Errorf("begin browser task heartbeat: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	var activeDevice string
	if err := tx.QueryRowContext(ctx, `UPDATE companion_devices SET last_seen_at = $2 WHERE id = $1 AND revoked_at IS NULL RETURNING id`, deviceID, now).Scan(&activeDevice); err == sql.ErrNoRows {
		return BrowserTask{}, apperror.New(401, apperror.AuthRequired, "Companion device authentication required", false)
	} else if err != nil {
		return BrowserTask{}, fmt.Errorf("lock companion device for heartbeat: %w", err)
	}
	row := tx.QueryRowContext(ctx, `UPDATE browser_tasks SET lease_expires_at = $1 WHERE id = $2 AND assigned_device_id = $3 AND lease_token_hash = $4 AND status = 'leased' AND lease_expires_at > $5 RETURNING id, site_id, target_url, status, assigned_device_id, lease_expires_at, attempt_count, message, balance, created_at, started_at, finished_at`, now.Add(2*time.Minute), taskID, deviceID, secretHash(leaseToken), now)
	task, err := scanTaskBare(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return BrowserTask{}, apperror.New(409, apperror.Conflict, "Browser task lease is not active", true)
		}
		return BrowserTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return BrowserTask{}, fmt.Errorf("commit browser task heartbeat: %w", err)
	}
	return task, nil
}

func (s *CompanionService) Finish(ctx context.Context, deviceID, taskID, leaseToken, status, message string, balance *string) (BrowserTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BrowserTask{}, fmt.Errorf("begin browser task finish: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	var activeDevice string
	if err := tx.QueryRowContext(ctx, `UPDATE companion_devices SET last_seen_at = $2 WHERE id = $1 AND revoked_at IS NULL RETURNING id`, deviceID, now).Scan(&activeDevice); err == sql.ErrNoRows {
		return BrowserTask{}, apperror.New(401, apperror.AuthRequired, "Companion device authentication required", false)
	} else if err != nil {
		return BrowserTask{}, fmt.Errorf("lock companion device for finish: %w", err)
	}
	leaseHash := secretHash(leaseToken)
	row := tx.QueryRowContext(ctx, `UPDATE browser_tasks SET status = $1, message = $2, balance = $3, lease_expires_at = NULL, finished_at = $4 WHERE id = $5 AND assigned_device_id = $6 AND lease_token_hash = $7 AND status = 'leased' AND lease_expires_at > $4 RETURNING id, site_id, target_url, status, assigned_device_id, lease_expires_at, attempt_count, message, balance, created_at, started_at, finished_at`, status, message, balance, now, taskID, deviceID, leaseHash)
	task, err := scanTaskBare(row)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return BrowserTask{}, fmt.Errorf("commit browser task finish: %w", err)
		}
		return task, nil
	} else if err != sql.ErrNoRows {
		return BrowserTask{}, err
	}
	row = tx.QueryRowContext(ctx, `SELECT id, site_id, target_url, status, assigned_device_id, lease_expires_at, attempt_count, message, balance, created_at, started_at, finished_at FROM browser_tasks WHERE id = $1 AND assigned_device_id = $2 AND lease_token_hash = $3`, taskID, deviceID, leaseHash)
	task, err = scanTaskBare(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return BrowserTask{}, apperror.New(409, apperror.Conflict, "Browser task lease is not active", true)
		}
		return BrowserTask{}, err
	}
	if task.Status == "success" || task.Status == "already_checked" || task.Status == "manual_required" || task.Status == "failed" {
		if err := tx.Commit(); err != nil {
			return BrowserTask{}, fmt.Errorf("commit idempotent browser task finish: %w", err)
		}
		return task, nil
	}
	return BrowserTask{}, apperror.New(409, apperror.Conflict, "Browser task lease is not active", true)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanTaskBare(row rowScanner) (BrowserTask, error) {
	var item BrowserTask
	var assigned sql.NullString
	var lease, started, finished sql.NullTime
	var balance sql.NullString
	var created time.Time
	err := row.Scan(&item.ID, &item.SiteID, &item.TargetURL, &item.Status, &assigned, &lease, &item.AttemptCount, &item.Message, &balance, &created, &started, &finished)
	if err != nil {
		return BrowserTask{}, err
	}
	item.CreatedAt = isoTime(created)
	if assigned.Valid {
		item.AssignedDeviceID = &assigned.String
	}
	if lease.Valid {
		item.LeaseExpiresAt = nullableISOTime(&lease.Time)
	}
	if balance.Valid {
		item.Balance = &balance.String
	}
	if started.Valid {
		item.StartedAt = nullableISOTime(&started.Time)
	}
	if finished.Valid {
		item.FinishedAt = nullableISOTime(&finished.Time)
	}
	return item, nil
}

func scanTaskJoined(row rowScanner) (BrowserTask, error) {
	var item BrowserTask
	var siteName, deviceName sql.NullString
	var assigned sql.NullString
	var lease, started, finished sql.NullTime
	var balance sql.NullString
	var created time.Time
	err := row.Scan(&item.ID, &item.SiteID, &siteName, &item.TargetURL, &item.Status, &assigned, &deviceName, &lease, &item.AttemptCount, &item.Message, &balance, &created, &started, &finished)
	if err != nil {
		return BrowserTask{}, err
	}
	item.SiteName = siteName.String
	item.AssignedDeviceName = deviceName.String
	item.CreatedAt = isoTime(created)
	if assigned.Valid {
		item.AssignedDeviceID = &assigned.String
	}
	if lease.Valid {
		item.LeaseExpiresAt = nullableISOTime(&lease.Time)
	}
	if balance.Valid {
		item.Balance = &balance.String
	}
	if started.Valid {
		item.StartedAt = nullableISOTime(&started.Time)
	}
	if finished.Valid {
		item.FinishedAt = nullableISOTime(&finished.Time)
	}
	return item, nil
}
