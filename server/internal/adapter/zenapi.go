package adapter

import (
	"context"

	"github.com/elykia/apihub/server/internal/domain"
	"github.com/elykia/apihub/server/internal/netclient"
)

type ZenAPIAdapter struct{ http *netclient.Client }

func NewZenAPI(http *netclient.Client) *ZenAPIAdapter { return &ZenAPIAdapter{http: http} }
func (a *ZenAPIAdapter) Descriptor() domain.AdapterDescriptor {
	return domain.AdapterDescriptor{Name: domain.ZenAPI, DisplayName: "ZenAPI", Capabilities: domain.Capabilities{Checkin: true}}
}
func (a *ZenAPIAdapter) CheckIn(ctx context.Context, site domain.SiteContext, _ string) (domain.CheckinResult, error) {
	response, err := a.http.RequestJSON(ctx, site.BaseURL, "/api/u/checkin", "POST", bearerHeaders(site.AccessToken), map[string]any{})
	if err != nil {
		return domain.CheckinResult{}, err
	}
	return parseGenericCheckin(response)
}
func (a *ZenAPIAdapter) FetchAnnouncements(context.Context, domain.SiteContext) (domain.AnnouncementResult, error) {
	return domain.AnnouncementResult{}, unsupported("ZenAPI", "announcement sync")
}
