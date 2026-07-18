package adapter

import (
	"context"
	"strings"

	"github.com/elykia/apihub/server/internal/apperror"
	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

type Sub2APIAdapter struct{ http *netclient.Client }

func NewSub2API(http *netclient.Client) *Sub2APIAdapter { return &Sub2APIAdapter{http: http} }
func (a *Sub2APIAdapter) Descriptor() domain.AdapterDescriptor {
	return domain.AdapterDescriptor{Name: domain.Sub2API, DisplayName: "Sub2API", Capabilities: domain.Capabilities{Announcements: true}}
}
func (a *Sub2APIAdapter) CheckIn(context.Context, domain.SiteContext, string) (domain.CheckinResult, error) {
	return domain.CheckinResult{}, unsupported("Sub2API", "server-side check-in")
}
func (a *Sub2APIAdapter) FetchAnnouncements(ctx context.Context, site domain.SiteContext) (domain.AnnouncementResult, error) {
	response, err := a.http.RequestJSON(ctx, site.BaseURL, "/api/v1/announcements?unread_only=1", "GET", bearerHeaders(site.AccessToken), nil)
	if err != nil {
		return domain.AnnouncementResult{}, err
	}
	payload, _ := record(response.JSON)
	if !response.OK || (has(payload, "code") && !numberEquals(payload["code"], 0)) {
		return domain.AnnouncementResult{}, apperror.New(502, apperror.UpstreamRejected, "Unable to fetch Sub2API announcements: "+responseMessage(response), response.Status >= 500)
	}
	var candidates []any
	switch data := payload["data"].(type) {
	case []any:
		candidates = data
	case map[string]any:
		candidates, _ = data["items"].([]any)
	}
	result := domain.AnnouncementResult{}
	for index, candidate := range candidates {
		if index >= 100 {
			break
		}
		item, ok := record(candidate)
		if !ok {
			continue
		}
		content := ""
		for _, key := range []string{"content", "message", "body"} {
			if content = stringValue(item[key]); content != "" {
				break
			}
		}
		title := stringValue(item["title"])
		if content == "" {
			content = title
		}
		if content == "" {
			continue
		}
		var extra *string
		if title != "" && title != content {
			value := strings.TrimSpace(title)
			extra = &value
		}
		published := item["updated_at"]
		if published == nil {
			published = item["created_at"]
		}
		result.Items = append(result.Items, domain.AnnouncementItem{Source: "notice", Content: content, Kind: "account", Extra: extra, PublishedAt: normalizePublishedAt(published)})
	}
	return result, nil
}
