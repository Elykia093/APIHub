package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	entsite "github.com/elykia/apihub/server/ent/site"
	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/importer/allhubapi"
)

const (
	allAPIHubImportSource         = "all-api-hub"
	importDefaultCheckinCron      = "15 8 * * *"
	importDefaultAnnouncementCron = "*/30 * * * *"
	importDefaultTimezone         = "Asia/Shanghai"
)

const (
	importStatusReady   = "ready"
	importStatusCreated = "created"
	importStatusSkipped = "skipped"
)

const (
	importReasonInvalidAccount      = "INVALID_ACCOUNT"
	importReasonUnsupportedSiteType = "UNSUPPORTED_SITE_TYPE"
	importReasonUnsupportedAuthType = "UNSUPPORTED_AUTH_TYPE"
	importReasonDuplicateInBackup   = "DUPLICATE_IN_BACKUP"
	importReasonAlreadyExists       = "ALREADY_EXISTS"
)

var allAPIHubNewAPIFamilyTypes = map[string]struct{}{
	"one-api": {}, "new-api": {}, "anyrouter": {}, "Veloera": {},
	"one-hub": {}, "done-hub": {}, "v-api": {}, "VoAPI": {},
	"Super-API": {}, "Rix-Api": {}, "neo-Api": {}, "wong-gongyi": {},
}

type SiteImportSummary struct {
	Total       int `json:"total"`
	Ready       int `json:"ready"`
	Created     int `json:"created"`
	Skipped     int `json:"skipped"`
	Duplicates  int `json:"duplicates"`
	Unsupported int `json:"unsupported"`
	Invalid     int `json:"invalid"`
}

type SiteImportItem struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	BaseURL    string `json:"baseUrl"`
	SiteType   string `json:"siteType"`
	Adapter    string `json:"adapter,omitempty"`
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type SiteImportResult struct {
	Source        string            `json:"source"`
	SourceVersion string            `json:"sourceVersion"`
	DryRun        bool              `json:"dryRun"`
	Summary       SiteImportSummary `json:"summary"`
	Items         []SiteImportItem  `json:"items"`
}

type siteImportCandidate struct {
	resultIndex int
	prepared    preparedSiteCreate
}

func (s *SiteService) ImportAllAPIHub(ctx context.Context, raw json.RawMessage, dryRun bool) (SiteImportResult, error) {
	backup, err := allhubapi.Parse(raw)
	if err != nil {
		return SiteImportResult{}, allAPIHubParseError(err)
	}
	result := SiteImportResult{
		Source:        allAPIHubImportSource,
		SourceVersion: backup.Version,
		DryRun:        dryRun,
		Summary:       SiteImportSummary{Total: len(backup.Accounts)},
		Items:         make([]SiteImportItem, 0, len(backup.Accounts)),
	}
	candidates := make([]siteImportCandidate, 0, len(backup.Accounts))
	seenURLs := make(map[string]struct{}, len(backup.Accounts))

	for _, account := range backup.Accounts {
		item := SiteImportItem{
			Index:    account.Index,
			Name:     previewText(account.Name, 120),
			BaseURL:  previewText(account.BaseURL, 256),
			SiteType: previewText(account.SiteType, 64),
			Status:   importStatusSkipped,
		}
		result.Items = append(result.Items, item)
		resultIndex := len(result.Items) - 1

		if account.ParseError != "" {
			markInvalidImport(&result, resultIndex, account.ParseError)
			continue
		}
		adapterName, supported := mapAllAPIHubAdapter(account.SiteType)
		if !supported {
			markSkippedImport(&result, resultIndex, importReasonUnsupportedSiteType, "Site type is not supported by APIHub")
			result.Summary.Unsupported++
			continue
		}
		result.Items[resultIndex].Adapter = adapterName
		if account.AuthType != "" && account.AuthType != "access_token" {
			markSkippedImport(&result, resultIndex, importReasonUnsupportedAuthType, "Only access-token accounts can be imported")
			result.Summary.Invalid++
			continue
		}

		checkinEnabled := account.CheckinEnabled
		announcementEnabled := true
		if adapterName == "sub2api" {
			checkinEnabled = false
		}
		input := CreateSiteInput{
			Name:                account.Name,
			BaseURL:             account.BaseURL,
			Adapter:             adapterName,
			UserID:              account.UserID,
			AccessToken:         account.AccessToken,
			Enabled:             !account.Disabled,
			CheckinEnabled:      &checkinEnabled,
			AnnouncementEnabled: &announcementEnabled,
			CheckinCron:         importDefaultCheckinCron,
			AnnouncementCron:    importDefaultAnnouncementCron,
			Timezone:            importDefaultTimezone,
		}
		prepared, prepareErr := s.prepareSiteCreate(ctx, input)
		if prepareErr != nil {
			appErr := apperror.As(prepareErr)
			markInvalidImport(&result, resultIndex, appErr.Message)
			continue
		}
		result.Items[resultIndex].Name = prepared.name
		result.Items[resultIndex].BaseURL = prepared.baseURL
		if _, duplicate := seenURLs[prepared.baseURL]; duplicate {
			markSkippedImport(&result, resultIndex, importReasonDuplicateInBackup, "A previous account in this backup has the same base URL")
			result.Summary.Duplicates++
			continue
		}
		seenURLs[prepared.baseURL] = struct{}{}
		result.Items[resultIndex].Status = importStatusReady
		candidates = append(candidates, siteImportCandidate{resultIndex: resultIndex, prepared: prepared})
	}

	if err := s.markExistingImportSites(ctx, &result, candidates); err != nil {
		return SiteImportResult{}, err
	}
	ready := make([]siteImportCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if result.Items[candidate.resultIndex].Status == importStatusReady {
			ready = append(ready, candidate)
		}
	}
	result.Summary.Ready = len(ready)
	result.Summary.Skipped = result.Summary.Total - result.Summary.Ready
	if dryRun || len(ready) == 0 {
		return result, nil
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return SiteImportResult{}, fmt.Errorf("begin site import transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, candidate := range ready {
		if _, err := s.createPreparedSite(ctx, tx.Client(), candidate.prepared); err != nil {
			return SiteImportResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SiteImportResult{}, fmt.Errorf("commit site import transaction: %w", err)
	}
	committed = true
	for _, candidate := range ready {
		result.Items[candidate.resultIndex].Status = importStatusCreated
	}
	result.Summary.Created = len(ready)
	return result, nil
}

func (s *SiteService) markExistingImportSites(ctx context.Context, result *SiteImportResult, candidates []siteImportCandidate) error {
	if len(candidates) == 0 {
		return nil
	}
	urls := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		urls = append(urls, candidate.prepared.baseURL)
	}
	existing, err := s.client.Site.Query().Where(entsite.BaseURLIn(urls...)).Select(entsite.FieldBaseURL).All(ctx)
	if err != nil {
		return fmt.Errorf("check existing import sites: %w", err)
	}
	existingURLs := make(map[string]struct{}, len(existing))
	for _, site := range existing {
		existingURLs[site.BaseURL] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := existingURLs[candidate.prepared.baseURL]; !ok {
			continue
		}
		markSkippedImport(result, candidate.resultIndex, importReasonAlreadyExists, "A site with the same base URL already exists")
		result.Summary.Duplicates++
	}
	return nil
}

func mapAllAPIHubAdapter(siteType string) (string, bool) {
	if siteType == "sub2api" {
		return "sub2api", true
	}
	_, ok := allAPIHubNewAPIFamilyTypes[siteType]
	return "new-api", ok
}

func allAPIHubParseError(err error) error {
	switch {
	case errors.Is(err, allhubapi.ErrUnsupportedVersion):
		return apperror.New(422, apperror.ValidationError, "Unsupported All API Hub backup version", false)
	case errors.Is(err, allhubapi.ErrNoAccounts):
		return apperror.New(422, apperror.ValidationError, "All API Hub backup contains no account records", false)
	case strings.Contains(err.Error(), "more than"):
		return apperror.New(422, apperror.ValidationError, fmt.Sprintf("All API Hub backup may contain at most %d accounts", allhubapi.MaxAccounts), false)
	default:
		return apperror.New(422, apperror.ValidationError, "Invalid All API Hub backup format", false)
	}
}

func markInvalidImport(result *SiteImportResult, index int, reason string) {
	markSkippedImport(result, index, importReasonInvalidAccount, reason)
	result.Summary.Invalid++
}

func markSkippedImport(result *SiteImportResult, index int, code, reason string) {
	result.Items[index].Status = importStatusSkipped
	result.Items[index].ReasonCode = code
	result.Items[index].Reason = reason
}

func previewText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
