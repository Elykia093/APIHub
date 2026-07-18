package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/elykia/apihub/server/internal/domain"
)

type ISOTime time.Time

func (value ISOTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(value).UTC().Format("2006-01-02T15:04:05.000Z"))
}

func isoTime(value time.Time) ISOTime { return ISOTime(value) }

func nullableISOTime(value *time.Time) *ISOTime {
	if value == nil {
		return nil
	}
	converted := isoTime(*value)
	return &converted
}

const terminalWriteTimeout = 5 * time.Second

func terminalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), terminalWriteTimeout)
}

type CreateSiteInput struct {
	Name, BaseURL, UserID, AccessToken, CheckinCron, AnnouncementCron, Timezone string
	Adapter                                                                     string
	Enabled                                                                     bool
	CheckinEnabled, AnnouncementEnabled                                         *bool
}

type PatchSiteInput struct {
	Name, BaseURL, Adapter, UserID, AccessToken, CheckinCron, AnnouncementCron, Timezone *string
	Enabled, CheckinEnabled, AnnouncementEnabled                                         *bool
}

type SiteView struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	BaseURL              string              `json:"baseUrl"`
	Adapter              string              `json:"adapter"`
	UserID               string              `json:"userId"`
	Enabled              bool                `json:"enabled"`
	CheckinEnabled       bool                `json:"checkinEnabled"`
	AnnouncementEnabled  bool                `json:"announcementEnabled"`
	CheckinCron          string              `json:"checkinCron"`
	AnnouncementCron     string              `json:"announcementCron"`
	Timezone             string              `json:"timezone"`
	CredentialConfigured bool                `json:"credentialConfigured"`
	ConsecutiveFailures  int                 `json:"consecutiveFailures"`
	Capabilities         domain.Capabilities `json:"capabilities"`
	CreatedAt            ISOTime             `json:"createdAt"`
	UpdatedAt            ISOTime             `json:"updatedAt"`
}

type CheckinView struct {
	ID           string   `json:"id"`
	SiteID       string   `json:"siteId"`
	SiteName     string   `json:"siteName,omitempty"`
	LocalDate    string   `json:"localDate"`
	Status       string   `json:"status"`
	RewardValue  *int64   `json:"rewardValue"`
	Message      string   `json:"message"`
	ErrorCode    *string  `json:"errorCode"`
	AttemptCount int      `json:"attemptCount"`
	StartedAt    ISOTime  `json:"startedAt"`
	FinishedAt   *ISOTime `json:"finishedAt"`
	RequestID    string   `json:"requestId"`
}

type AnnouncementView struct {
	ID          string   `json:"id"`
	SiteID      string   `json:"siteId"`
	SiteName    string   `json:"siteName,omitempty"`
	Source      string   `json:"source"`
	Fingerprint string   `json:"fingerprint"`
	Content     string   `json:"content"`
	Kind        string   `json:"kind"`
	Extra       *string  `json:"extra"`
	PublishedAt *ISOTime `json:"publishedAt"`
	FirstSeenAt ISOTime  `json:"firstSeenAt"`
	LastSeenAt  ISOTime  `json:"lastSeenAt"`
	ReadAt      *ISOTime `json:"readAt"`
}

type AnnouncementSyncView struct {
	ID         string   `json:"id"`
	SiteID     string   `json:"siteId"`
	Status     string   `json:"status"`
	AddedCount int      `json:"addedCount"`
	Message    string   `json:"message"`
	StartedAt  ISOTime  `json:"startedAt"`
	FinishedAt *ISOTime `json:"finishedAt"`
	RequestID  string   `json:"requestId"`
}

type SiteCounts struct {
	Total   int `json:"total"`
	Enabled int `json:"enabled"`
}

type Summary struct {
	Sites               SiteCounts     `json:"sites"`
	Today               map[string]int `json:"today"`
	UnreadAnnouncements int            `json:"unreadAnnouncements"`
}
