package adapter

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

type NewAPIAdapter struct{ http *netclient.Client }

func NewNewAPI(http *netclient.Client) *NewAPIAdapter { return &NewAPIAdapter{http: http} }
func (a *NewAPIAdapter) Descriptor() domain.AdapterDescriptor {
	return domain.AdapterDescriptor{Name: domain.NewAPI, DisplayName: "New API", Capabilities: domain.Capabilities{Checkin: true, Announcements: true, RequiresUserID: true}}
}

func (a *NewAPIAdapter) CheckIn(ctx context.Context, site domain.SiteContext, localDate string) (domain.CheckinResult, error) {
	publicStatus, err := a.http.RequestJSON(ctx, site.BaseURL, "/api/status", "GET", nil, nil)
	if err != nil {
		return domain.CheckinResult{}, err
	}
	if publicStatus.OK {
		payload, _ := record(publicStatus.JSON)
		data, _ := record(payload["data"])
		if !boolValue(payload["success"]) {
			data = nil
		}
		if enabled, exists := data["checkin_enabled"].(bool); exists && !enabled {
			return domain.CheckinResult{}, apperror.New(502, apperror.UpstreamRejected, "Upstream site has disabled check-in", false)
		}
		if boolValue(data["turnstile_check"]) {
			return domain.CheckinResult{Status: "manual_required", Message: "Upstream site requires Turnstile or CAPTCHA verification"}, nil
		}
	}
	headers := map[string]string{"Accept": "application/json", "Content-Type": "application/json", "Authorization": site.AccessToken, "New-Api-User": site.UserID}
	month := localDate[:7]
	status, err := a.http.RequestJSON(ctx, site.BaseURL, "/api/user/checkin?month="+url.QueryEscape(month), "GET", headers, nil)
	if err != nil {
		return domain.CheckinResult{}, err
	}
	payload, _ := record(status.JSON)
	if !status.OK || !boolValue(payload["success"]) {
		message := responseMessage(status)
		if alreadyPattern.MatchString(message) {
			return domain.CheckinResult{Status: "already_checked", Message: message}, nil
		}
		if manualPattern.MatchString(message) {
			return domain.CheckinResult{Status: "manual_required", Message: message}, nil
		}
		return domain.CheckinResult{}, apperror.New(502, apperror.UpstreamRejected, "Unable to read check-in status: "+message, status.Status >= 500)
	}
	data, _ := record(payload["data"])
	stats, _ := record(data["stats"])
	if boolValue(stats["checked_in_today"]) {
		return domain.CheckinResult{Status: "already_checked", Message: "Already checked in today"}, nil
	}
	result, err := a.http.RequestJSON(ctx, site.BaseURL, "/api/user/checkin", "POST", headers, map[string]any{})
	if err != nil {
		return domain.CheckinResult{}, err
	}
	resultPayload, _ := record(result.JSON)
	message := responseMessage(result)
	if !result.OK || !boolValue(resultPayload["success"]) {
		if alreadyPattern.MatchString(message) {
			return domain.CheckinResult{Status: "already_checked", Message: message}, nil
		}
		if manualPattern.MatchString(message) {
			return domain.CheckinResult{Status: "manual_required", Message: message}, nil
		}
		return domain.CheckinResult{}, apperror.New(502, apperror.UpstreamRejected, "Check-in was rejected: "+message, result.Status >= 500)
	}
	resultData, _ := record(resultPayload["data"])
	var reward *int64
	if value, ok := safeInt64(resultData["quota_awarded"]); ok {
		reward = &value
	}
	if message == "" {
		message = "Check-in succeeded"
	}
	return domain.CheckinResult{Status: "success", RewardValue: reward, Message: message}, nil
}

func (a *NewAPIAdapter) FetchAnnouncements(ctx context.Context, site domain.SiteContext) (domain.AnnouncementResult, error) {
	type result struct {
		response netclient.Response
		err      error
	}
	statusChannel, noticeChannel := make(chan result, 1), make(chan result, 1)
	go func() {
		output := result{}
		defer func() {
			if recovered := recover(); recovered != nil {
				output.err = fmt.Errorf("status announcement request panic: %v", recovered)
			}
			statusChannel <- output
		}()
		output.response, output.err = a.http.RequestJSON(ctx, site.BaseURL, "/api/status", "GET", nil, nil)
	}()
	go func() {
		output := result{}
		defer func() {
			if recovered := recover(); recovered != nil {
				output.err = fmt.Errorf("notice announcement request panic: %v", recovered)
			}
			noticeChannel <- output
		}()
		output.response, output.err = a.http.RequestJSON(ctx, site.BaseURL, "/api/notice", "GET", nil, nil)
	}()
	status, notice := <-statusChannel, <-noticeChannel
	output := domain.AnnouncementResult{}
	warnings := make([]string, 0, 2)
	if status.err == nil && status.response.OK {
		payload, _ := record(status.response.JSON)
		data, _ := record(payload["data"])
		if boolValue(payload["success"]) {
			if enabled, exists := data["announcements_enabled"].(bool); !exists || enabled {
				if items, ok := data["announcements"].([]any); ok {
					for index, candidate := range items {
						if index >= 100 {
							break
						}
						item, ok := record(candidate)
						if !ok {
							continue
						}
						content := stringValue(item["content"])
						if content == "" {
							continue
						}
						kind := stringValue(item["type"])
						if kind == "" {
							kind = "default"
						}
						kind = truncate(kind, 32)
						var extra *string
						if value := stringValue(item["extra"]); value != "" {
							extra = &value
						}
						output.Items = append(output.Items, domain.AnnouncementItem{Source: "status", Content: content, Kind: kind, Extra: extra, PublishedAt: normalizePublishedAt(item["publishDate"])})
					}
				}
			}
		} else {
			warnings = append(warnings, "status="+announcementFailureCode(nil, status.response))
		}
	} else {
		warnings = append(warnings, "status="+announcementFailureCode(status.err, status.response))
	}
	if notice.err == nil && notice.response.OK {
		payload, _ := record(notice.response.JSON)
		if boolValue(payload["success"]) {
			if content := stringValue(payload["data"]); content != "" {
				output.Items = append(output.Items, domain.AnnouncementItem{Source: "notice", Content: content, Kind: "default"})
			}
		} else {
			warnings = append(warnings, "notice="+announcementFailureCode(nil, notice.response))
		}
	} else {
		warnings = append(warnings, "notice="+announcementFailureCode(notice.err, notice.response))
	}
	output.Warnings = warnings
	if len(warnings) == 2 {
		return domain.AnnouncementResult{}, apperror.New(502, apperror.UpstreamRejected, "All announcement sources failed: "+strings.Join(warnings, "; "), true)
	}
	return output, nil
}

func announcementFailureCode(err error, response netclient.Response) string {
	if err != nil {
		return apperror.As(err).Code
	}
	if response.Status != 0 && !response.OK {
		return fmt.Sprintf("HTTP_%d", response.Status)
	}
	return apperror.UpstreamRejected
}
