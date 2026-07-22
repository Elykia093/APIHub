package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elykia/apihub/server/internal/adapter"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/config"
	"github.com/elykia/apihub/server/internal/cryptoutil"
	"github.com/elykia/apihub/server/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Dependencies struct {
	Config        config.Config
	DB            *sql.DB
	Sites         *service.SiteService
	Checkins      *service.CheckinService
	Announcements *service.AnnouncementService
	Companion     *service.CompanionService
	Scheduler     *service.Scheduler
	Adapters      *adapter.Registry
	Logger        *slog.Logger
	Web           fs.FS
}

type handler struct{ dependencies Dependencies }

var bearerAuthorization = regexp.MustCompile(`(?i)^Bearer\s+(.+)$`)
var canonicalUUID = regexp.MustCompile(`(?i)^(?:00000000-0000-0000-0000-000000000000|[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$`)
var dedicatedRateLimitRoute = regexp.MustCompile(`^/api/v1/sites/[^/]+/(?:checkin-runs|announcement-syncs)$`)

func NewRouter(dependencies Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.RedirectTrailingSlash = false
	h := &handler{dependencies: dependencies}
	globalLimiter := newLimiter(240, time.Minute).middleware()
	router.Use(h.requestID(), h.recovery(), securityHeaders(), func(c *gin.Context) {
		apiPath := c.Request.URL.Path
		isCompanion := strings.HasPrefix(apiPath, "/api/v1/companion/")
		if isCompanion {
			c.Header("Cache-Control", "no-store")
		}
		if strings.HasPrefix(apiPath, "/api/v1/") && !isCompanion && !authorized(c.GetHeader("Authorization"), dependencies.Config.AdminToken) {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodPost && dedicatedRateLimitRoute.MatchString(apiPath) {
			c.Next()
			return
		}
		globalLimiter(c)
	}, h.accessLog())
	live := func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) }
	router.Match([]string{http.MethodGet, http.MethodHead}, "/health/live", live)
	router.Match([]string{http.MethodGet, http.MethodHead}, "/health/ready", h.ready)
	management := router.Group("/api/v1")
	management.Use(h.authenticate())
	readMethods := []string{http.MethodGet, http.MethodHead}
	management.Match(readMethods, "/summary", h.summary)
	management.Match(readMethods, "/site-adapters", h.siteAdapters)
	management.Match(readMethods, "/sites", h.listSites)
	management.Match(readMethods, "/sites/", h.getSite)
	management.Match(readMethods, "/sites/:siteId", h.getSite)
	management.POST("/sites", h.createSite)
	management.PATCH("/sites/:siteId", h.patchSite)
	management.POST("/sites/:siteId/checkin-runs", newLimiter(10, time.Minute).middleware(), h.runCheckin)
	management.Match(readMethods, "/checkin-runs", h.listCheckins)
	management.POST("/sites/:siteId/announcement-syncs", newLimiter(20, time.Minute).middleware(), h.syncAnnouncements)
	management.Match(readMethods, "/announcements", h.listAnnouncements)
	management.PATCH("/announcements/:announcementId", h.setAnnouncementRead)
	management.POST("/companion-pairing-codes", h.createCompanionPairingCode)
	management.Match(readMethods, "/companion-devices", h.listCompanionDevices)
	management.POST("/companion-devices/:deviceId/revocations", h.revokeCompanionDevice)
	management.POST("/sites/:siteId/browser-tasks", h.createBrowserTask)
	management.Match(readMethods, "/browser-tasks", h.listBrowserTasks)
	companion := router.Group("/api/v1/companion")
	companion.POST("/pairings", newLimiter(10, 5*time.Minute).middleware(), h.pairCompanion)
	companion.POST("/tasks/claims", h.claimBrowserTask)
	companion.POST("/tasks/:taskId/heartbeats", h.heartbeatBrowserTask)
	companion.POST("/tasks/:taskId/results", h.finishBrowserTask)
	router.NoRoute(h.serveWeb)
	return router
}

func (h *handler) ready(c *gin.Context) {
	tx, err := h.dependencies.DB.BeginTx(c.Request.Context(), nil)
	if err != nil {
		h.fail(c, fmt.Errorf("begin readiness transaction: %w", err))
		return
	}
	rollbackComplete := false
	defer func() {
		if !rollbackComplete {
			_ = tx.Rollback()
		}
	}()
	var value int
	if err := tx.QueryRowContext(c.Request.Context(), "SELECT 1").Scan(&value); err != nil || value != 1 {
		if err == nil {
			err = fmt.Errorf("unexpected readiness value")
		}
		h.fail(c, fmt.Errorf("readiness read: %w", err))
		return
	}
	if _, err := tx.ExecContext(c.Request.Context(), "UPDATE sites SET updated_at = updated_at WHERE FALSE"); err != nil {
		h.fail(c, fmt.Errorf("readiness write: %w", err))
		return
	}
	if err := tx.Rollback(); err != nil {
		h.fail(c, fmt.Errorf("rollback readiness transaction: %w", err))
		return
	}
	rollbackComplete = true
	c.JSON(200, gin.H{"status": "ready"})
}

func (h *handler) summary(c *gin.Context) {
	result, err := h.dependencies.Sites.Summary(c.Request.Context(), time.Now())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *handler) siteAdapters(c *gin.Context) {
	c.JSON(200, gin.H{"data": h.dependencies.Adapters.List()})
}
func (h *handler) listSites(c *gin.Context) {
	result, err := h.dependencies.Sites.List(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *handler) getSite(c *gin.Context) {
	id, ok := h.uuidParam(c, "siteId")
	if !ok {
		return
	}
	result, err := h.dependencies.Sites.Get(c.Request.Context(), id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

type createSiteRequest struct {
	Name                string  `json:"name"`
	BaseURL             string  `json:"baseUrl"`
	Adapter             *string `json:"adapter"`
	UserID              string  `json:"userId"`
	AccessToken         string  `json:"accessToken"`
	Enabled             *bool   `json:"enabled"`
	CheckinEnabled      *bool   `json:"checkinEnabled"`
	AnnouncementEnabled *bool   `json:"announcementEnabled"`
	CheckinCron         *string `json:"checkinCron"`
	AnnouncementCron    *string `json:"announcementCron"`
	Timezone            *string `json:"timezone"`
}

func (h *handler) createSite(c *gin.Context) {
	var request createSiteRequest
	if !h.decode(c, &request, "name", "baseUrl", "adapter", "userId", "accessToken", "enabled", "checkinEnabled", "announcementEnabled", "checkinCron", "announcementCron", "timezone") {
		return
	}
	for name, value := range map[string]*string{
		"adapter": request.Adapter, "checkinCron": request.CheckinCron,
		"announcementCron": request.AnnouncementCron, "timezone": request.Timezone,
	} {
		if value != nil && strings.TrimSpace(*value) == "" {
			h.fail(c, apperror.New(422, apperror.ValidationError, name+" must not be empty", false))
			return
		}
	}
	adapterName := "new-api"
	if request.Adapter != nil {
		adapterName = *request.Adapter
	}
	checkinCron := "15 8 * * *"
	if request.CheckinCron != nil {
		checkinCron = *request.CheckinCron
	}
	announcementCron := "*/30 * * * *"
	if request.AnnouncementCron != nil {
		announcementCron = *request.AnnouncementCron
	}
	timezone := "Asia/Shanghai"
	if request.Timezone != nil {
		timezone = *request.Timezone
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	result, err := h.dependencies.Sites.Create(c.Request.Context(), service.CreateSiteInput{Name: request.Name, BaseURL: request.BaseURL, Adapter: adapterName, UserID: request.UserID, AccessToken: request.AccessToken, Enabled: enabled, CheckinEnabled: request.CheckinEnabled, AnnouncementEnabled: request.AnnouncementEnabled, CheckinCron: checkinCron, AnnouncementCron: announcementCron, Timezone: timezone})
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.dependencies.Scheduler.Reload(c.Request.Context()); err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Location", "/api/v1/sites/"+result.ID)
	c.JSON(201, gin.H{"data": result})
}

type patchSiteRequest struct {
	Name                *string `json:"name"`
	BaseURL             *string `json:"baseUrl"`
	Adapter             *string `json:"adapter"`
	UserID              *string `json:"userId"`
	AccessToken         *string `json:"accessToken"`
	Enabled             *bool   `json:"enabled"`
	CheckinEnabled      *bool   `json:"checkinEnabled"`
	AnnouncementEnabled *bool   `json:"announcementEnabled"`
	CheckinCron         *string `json:"checkinCron"`
	AnnouncementCron    *string `json:"announcementCron"`
	Timezone            *string `json:"timezone"`
}

func (h *handler) patchSite(c *gin.Context) {
	id, ok := h.uuidParam(c, "siteId")
	if !ok {
		return
	}
	var request patchSiteRequest
	if !h.decode(c, &request, "name", "baseUrl", "adapter", "userId", "accessToken", "enabled", "checkinEnabled", "announcementEnabled", "checkinCron", "announcementCron", "timezone") {
		return
	}
	if request.empty() {
		h.fail(c, apperror.New(422, apperror.ValidationError, "At least one field must be supplied", false))
		return
	}
	result, err := h.dependencies.Sites.Patch(c.Request.Context(), id, service.PatchSiteInput{Name: request.Name, BaseURL: request.BaseURL, Adapter: request.Adapter, UserID: request.UserID, AccessToken: request.AccessToken, Enabled: request.Enabled, CheckinEnabled: request.CheckinEnabled, AnnouncementEnabled: request.AnnouncementEnabled, CheckinCron: request.CheckinCron, AnnouncementCron: request.AnnouncementCron, Timezone: request.Timezone})
	if err != nil {
		h.fail(c, err)
		return
	}
	if err := h.dependencies.Scheduler.Reload(c.Request.Context()); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (r patchSiteRequest) empty() bool {
	return r.Name == nil && r.BaseURL == nil && r.Adapter == nil && r.UserID == nil && r.AccessToken == nil && r.Enabled == nil && r.CheckinEnabled == nil && r.AnnouncementEnabled == nil && r.CheckinCron == nil && r.AnnouncementCron == nil && r.Timezone == nil
}

func (h *handler) runCheckin(c *gin.Context) {
	id, ok := h.uuidParam(c, "siteId")
	if !ok {
		return
	}
	requestID := requestID(c)
	result, err := h.dependencies.Checkins.Run(c.Request.Context(), id, requestID, time.Now())
	if err != nil {
		h.fail(c, err)
		return
	}
	status := 200
	if result.RequestID == requestID {
		status = 201
	}
	c.JSON(status, gin.H{"data": result})
}
func (h *handler) listCheckins(c *gin.Context) {
	limit, siteID, ok := h.listQuery(c)
	if !ok {
		return
	}
	result, err := h.dependencies.Checkins.List(c.Request.Context(), siteID, limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *handler) syncAnnouncements(c *gin.Context) {
	id, ok := h.uuidParam(c, "siteId")
	if !ok {
		return
	}
	result, err := h.dependencies.Announcements.Sync(c.Request.Context(), id, requestID(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": result})
}
func (h *handler) listAnnouncements(c *gin.Context) {
	limit, siteID, ok := h.listQuery(c)
	if !ok {
		return
	}
	result, err := h.dependencies.Announcements.List(c.Request.Context(), siteID, limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *handler) setAnnouncementRead(c *gin.Context) {
	id, ok := h.uuidParam(c, "announcementId")
	if !ok {
		return
	}
	var request struct {
		Read *bool `json:"read"`
	}
	if !h.decode(c, &request, "read") {
		return
	}
	if request.Read == nil {
		h.fail(c, apperror.New(422, apperror.ValidationError, "read is required", false))
		return
	}
	result, err := h.dependencies.Announcements.SetRead(c.Request.Context(), id, *request.Read)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *handler) createCompanionPairingCode(c *gin.Context) {
	var request struct{}
	if !h.decode(c, &request) {
		return
	}
	code, expiresAt, err := h.dependencies.Companion.CreatePairingCode(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"code": code, "expiresAt": expiresAt}})
}

func (h *handler) listCompanionDevices(c *gin.Context) {
	result, err := h.dependencies.Companion.ListDevices(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *handler) revokeCompanionDevice(c *gin.Context) {
	id, ok := h.uuidParam(c, "deviceId")
	if !ok {
		return
	}
	if err := h.dependencies.Companion.RevokeDevice(c.Request.Context(), id); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"id": id, "revoked": true}})
}

func (h *handler) createBrowserTask(c *gin.Context) {
	siteID, ok := h.uuidParam(c, "siteId")
	if !ok {
		return
	}
	var request struct {
		TargetURL string `json:"targetUrl"`
	}
	if !h.decode(c, &request, "targetUrl") {
		return
	}
	if strings.TrimSpace(request.TargetURL) == "" || len([]rune(request.TargetURL)) > 2048 {
		h.fail(c, apperror.New(422, apperror.ValidationError, "targetUrl must contain 1 to 2048 characters", false))
		return
	}
	result, err := h.dependencies.Companion.CreateTask(c.Request.Context(), siteID, request.TargetURL)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": result})
}

func (h *handler) listBrowserTasks(c *gin.Context) {
	query := c.Request.URL.Query()
	for key := range query {
		if key != "limit" {
			h.fail(c, apperror.New(422, apperror.ValidationError, "Unknown query field: "+key, false))
			return
		}
	}
	limit := 50
	if values, present := query["limit"]; present {
		if len(values) != 1 {
			h.fail(c, apperror.New(422, apperror.ValidationError, "limit must be between 1 and 100", false))
			return
		}
		parsed, valid := parseBoundedInteger(values[0], 1, 100)
		if !valid {
			h.fail(c, apperror.New(422, apperror.ValidationError, "limit must be between 1 and 100", false))
			return
		}
		limit = parsed
	}
	result, err := h.dependencies.Companion.ListTasks(c.Request.Context(), limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *handler) pairCompanion(c *gin.Context) {
	var request struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
	}
	if !h.decode(c, &request, "code", "deviceName") {
		return
	}
	if strings.TrimSpace(request.Code) == "" || len([]rune(request.Code)) > 64 {
		h.fail(c, apperror.New(401, apperror.AuthRequired, "Pairing code is invalid or expired", false))
		return
	}
	device, token, err := h.dependencies.Companion.Pair(c.Request.Context(), request.Code, request.DeviceName)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": gin.H{"device": device, "deviceToken": token}})
}

func (h *handler) companionDevice(c *gin.Context) (service.CompanionDevice, bool) {
	match := bearerAuthorization.FindStringSubmatch(c.GetHeader("Authorization"))
	if len(match) != 2 {
		h.fail(c, apperror.New(401, apperror.AuthRequired, "Companion device authentication required", false))
		return service.CompanionDevice{}, false
	}
	device, err := h.dependencies.Companion.Authenticate(c.Request.Context(), match[1])
	if err != nil {
		h.fail(c, err)
		return service.CompanionDevice{}, false
	}
	return device, true
}

func (h *handler) claimBrowserTask(c *gin.Context) {
	device, ok := h.companionDevice(c)
	if !ok {
		return
	}
	claimed, err := h.dependencies.Companion.Claim(c.Request.Context(), device.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	if claimed == nil {
		c.Status(http.StatusNoContent)
		return
	}
	task := claimed.Task
	c.JSON(200, gin.H{"data": gin.H{"id": task.ID, "siteId": task.SiteID, "siteName": task.SiteName, "targetUrl": task.TargetURL, "status": task.Status, "attemptCount": task.AttemptCount, "leaseToken": claimed.LeaseToken}})
}

func (h *handler) companionLease(c *gin.Context) (service.CompanionDevice, string, bool) {
	device, ok := h.companionDevice(c)
	if !ok {
		return service.CompanionDevice{}, "", false
	}
	lease := c.GetHeader("X-Companion-Lease")
	if len(lease) < 32 || len(lease) > 128 {
		h.fail(c, apperror.New(401, apperror.AuthRequired, "Browser task lease is required", false))
		return service.CompanionDevice{}, "", false
	}
	return device, lease, true
}

func (h *handler) heartbeatBrowserTask(c *gin.Context) {
	taskID, ok := h.uuidParam(c, "taskId")
	if !ok {
		return
	}
	device, lease, ok := h.companionLease(c)
	if !ok {
		return
	}
	result, err := h.dependencies.Companion.Heartbeat(c.Request.Context(), device.ID, taskID, lease)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *handler) finishBrowserTask(c *gin.Context) {
	taskID, ok := h.uuidParam(c, "taskId")
	if !ok {
		return
	}
	device, ok := h.companionDevice(c)
	if !ok {
		return
	}
	var request struct {
		LeaseToken string  `json:"leaseToken"`
		Status     string  `json:"status"`
		Message    string  `json:"message"`
		Balance    *string `json:"balance"`
	}
	if !h.decode(c, &request, "leaseToken", "status", "message", "balance") {
		return
	}
	if len(request.LeaseToken) < 32 || len(request.LeaseToken) > 128 {
		h.fail(c, apperror.New(401, apperror.AuthRequired, "Browser task lease is invalid", false))
		return
	}
	if request.Status != "success" && request.Status != "already_checked" && request.Status != "manual_required" && request.Status != "failed" {
		h.fail(c, apperror.New(422, apperror.ValidationError, "Invalid browser task status", false))
		return
	}
	if len([]rune(request.Message)) > 500 {
		h.fail(c, apperror.New(422, apperror.ValidationError, "message must contain at most 500 characters", false))
		return
	}
	if request.Balance != nil && len([]rune(*request.Balance)) > 128 {
		h.fail(c, apperror.New(422, apperror.ValidationError, "balance must contain at most 128 characters", false))
		return
	}
	result, err := h.dependencies.Companion.Finish(c.Request.Context(), device.ID, taskID, request.LeaseToken, request.Status, request.Message, request.Balance)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (h *handler) decode(c *gin.Context, target any, allowedFields ...string) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		h.fail(c, apperror.New(415, apperror.UnsupportedMediaType, "Unsupported request media type", false))
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.fail(c, apperror.New(413, apperror.PayloadTooLarge, "Request body is too large", false))
			return false
		}
		h.fail(c, apperror.New(400, apperror.BadRequest, "Malformed request", false))
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.fail(c, apperror.New(413, apperror.PayloadTooLarge, "Request body is too large", false))
			return false
		}
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			h.fail(c, apperror.New(400, apperror.BadRequest, "Malformed request", false))
			return false
		}
		h.fail(c, apperror.New(422, apperror.ValidationError, err.Error(), false))
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		h.fail(c, apperror.New(422, apperror.ValidationError, "Request body must be an object", false))
		return false
	}
	allowed := make(map[string]struct{}, len(allowedFields))
	for _, name := range allowedFields {
		allowed[name] = struct{}{}
	}
	for name, value := range fields {
		if _, ok := allowed[name]; !ok {
			h.fail(c, apperror.New(422, apperror.ValidationError, "Unknown body field: "+name, false))
			return false
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			h.fail(c, apperror.New(422, apperror.ValidationError, name+" must not be null", false))
			return false
		}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.fail(c, apperror.New(400, apperror.BadRequest, "Malformed request", false))
		return false
	}
	return true
}
func (h *handler) listQuery(c *gin.Context) (int, *string, bool) {
	query := c.Request.URL.Query()
	for key := range query {
		if key != "limit" && key != "siteId" {
			h.fail(c, apperror.New(422, apperror.ValidationError, "Unknown query field: "+key, false))
			return 0, nil, false
		}
	}
	limit := 50
	if values, present := query["limit"]; present {
		if len(values) != 1 || values[0] == "" {
			h.fail(c, apperror.New(422, apperror.ValidationError, "limit must be between 1 and 100", false))
			return 0, nil, false
		}
		parsed, ok := parseBoundedInteger(values[0], 1, 100)
		if !ok {
			h.fail(c, apperror.New(422, apperror.ValidationError, "limit must be between 1 and 100", false))
			return 0, nil, false
		}
		limit = parsed
	}
	var siteID *string
	if values, present := query["siteId"]; present {
		if len(values) != 1 || values[0] == "" {
			h.fail(c, apperror.New(422, apperror.ValidationError, "siteId: Invalid UUID", false))
			return 0, nil, false
		}
		raw := values[0]
		if !validUUID(raw) {
			h.fail(c, apperror.New(422, apperror.ValidationError, "siteId: Invalid UUID", false))
			return 0, nil, false
		}
		siteID = &raw
	}
	return limit, siteID, true
}

func parseBoundedInteger(raw string, minimum, maximum int) (int, bool) {
	number, ok := config.ParseJavaScriptInteger(raw)
	if !ok || number < minimum || number > maximum {
		return 0, false
	}
	return number, true
}

func (h *handler) uuidParam(c *gin.Context, name string) (string, bool) {
	value := c.Param(name)
	if !validUUID(value) {
		h.fail(c, apperror.New(422, apperror.ValidationError, name+": Invalid UUID", false))
		return "", false
	}
	return value, true
}

func validUUID(value string) bool { return canonicalUUID.MatchString(value) }

func (h *handler) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !authorized(c.GetHeader("Authorization"), h.dependencies.Config.AdminToken) {
			h.fail(c, apperror.New(401, apperror.AuthRequired, "Administrator authentication required", false))
			c.Abort()
			return
		}
		c.Next()
	}
}
func (h *handler) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.NewString()
		c.Set("requestId", id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}
func requestID(c *gin.Context) string {
	value, _ := c.Get("requestId")
	id, _ := value.(string)
	return id
}
func (h *handler) recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		h.dependencies.Logger.ErrorContext(c.Request.Context(), "request panic", "requestId", requestID(c), "panic", fmt.Sprint(recovered))
		h.fail(c, apperror.New(500, apperror.InternalError, "Internal server error", false))
		c.Abort()
	})
}
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := map[string]string{
			"Content-Security-Policy":           "default-src 'self'; base-uri 'self'; font-src 'self' https: data:; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; script-src-attr 'none'; style-src 'self'; upgrade-insecure-requests; connect-src 'self'",
			"Cross-Origin-Opener-Policy":        "same-origin",
			"Cross-Origin-Resource-Policy":      "same-origin",
			"Origin-Agent-Cluster":              "?1",
			"Permissions-Policy":                "camera=(), microphone=(), geolocation=()",
			"Referrer-Policy":                   "no-referrer",
			"Strict-Transport-Security":         "max-age=31536000; includeSubDomains",
			"X-Content-Type-Options":            "nosniff",
			"X-DNS-Prefetch-Control":            "off",
			"X-Download-Options":                "noopen",
			"X-Frame-Options":                   "SAMEORIGIN",
			"X-Permitted-Cross-Domain-Policies": "none",
			"X-XSS-Protection":                  "0",
		}
		for key, value := range headers {
			c.Header(key, value)
		}
		c.Next()
	}
}
func (h *handler) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		h.dependencies.Logger.InfoContext(c.Request.Context(), "http request", "requestId", requestID(c), "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "durationMs", time.Since(started).Milliseconds())
	}
}

func (h *handler) fail(c *gin.Context, err error) {
	appErr := apperror.As(err)
	if appErr.Status >= 500 {
		h.dependencies.Logger.ErrorContext(c.Request.Context(), "request failed", "requestId", requestID(c), "errorCode", appErr.Code, "error", err)
	}
	c.JSON(appErr.Status, gin.H{"error": gin.H{"code": appErr.Code, "message": appErr.Message, "retryable": appErr.Retryable, "requestId": requestID(c)}})
}
func (h *handler) serveWeb(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/v1/") {
		c.Header("Cache-Control", "no-store")
		if !authorized(c.GetHeader("Authorization"), h.dependencies.Config.AdminToken) {
			h.fail(c, apperror.New(401, apperror.AuthRequired, "Administrator authentication required", false))
			return
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/health/") {
		h.fail(c, apperror.New(404, apperror.NotFound, "Route not found", false))
		return
	}
	requested := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
	if requested == "." || requested == "" {
		requested = "index.html"
	}
	payload, err := fs.ReadFile(h.dependencies.Web, requested)
	fallback := false
	if err != nil {
		payload, err = fs.ReadFile(h.dependencies.Web, "index.html")
		fallback = true
	}
	if err != nil {
		h.fail(c, fmt.Errorf("read embedded web asset: %w", err))
		return
	}
	contentType := mime.TypeByExtension(path.Ext(requested))
	if fallback {
		contentType = "text/html; charset=utf-8"
	}
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	c.Header("Cache-Control", "public, max-age=0")
	c.Data(200, contentType, payload)
}

func authorized(header, expected string) bool {
	match := bearerAuthorization.FindStringSubmatch(header)
	return len(match) == 2 && cryptoutil.TokensEqual(match[1], expected)
}

type bucket struct {
	window time.Time
	count  int
}
type limiter struct {
	max     int
	window  time.Duration
	mu      sync.Mutex
	clients map[string]bucket
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, clients: map[string]bucket{}}
}
func (l *limiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			host = c.Request.RemoteAddr
		}
		now := time.Now()
		l.mu.Lock()
		entry := l.clients[host]
		if entry.window.IsZero() || now.Sub(entry.window) >= l.window {
			entry = bucket{window: now}
		}
		entry.count++
		l.clients[host] = entry
		if len(l.clients) > 10000 {
			for key, value := range l.clients {
				if now.Sub(value.window) >= l.window {
					delete(l.clients, key)
				}
			}
		}
		limited := entry.count > l.max
		remaining := max(0, l.max-entry.count)
		reset := max(1, int(l.window.Seconds()-now.Sub(entry.window).Seconds()))
		l.mu.Unlock()
		c.Header("X-RateLimit-Limit", strconv.Itoa(l.max))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.Itoa(reset))
		if limited {
			c.Header("Retry-After", strconv.Itoa(reset))
			appErr := apperror.New(429, apperror.RateLimited, "Too many requests", true)
			c.JSON(429, gin.H{"error": gin.H{"code": appErr.Code, "message": appErr.Message, "retryable": true, "requestId": requestID(c)}})
			c.Abort()
			return
		}
		c.Next()
	}
}
