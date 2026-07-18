package domain

import "time"

type AdapterName string

const (
	NewAPI  AdapterName = "new-api"
	Sub2API AdapterName = "sub2api"
	ZenAPI  AdapterName = "zen-api"
)

type Capabilities struct {
	Checkin        bool `json:"checkin"`
	Announcements  bool `json:"announcements"`
	RequiresUserID bool `json:"requiresUserId"`
}

type AdapterDescriptor struct {
	Name         AdapterName  `json:"name"`
	DisplayName  string       `json:"displayName"`
	Capabilities Capabilities `json:"capabilities"`
}

type SiteContext struct {
	BaseURL, UserID, AccessToken, Timezone string
}

type CheckinResult struct {
	Status      string
	RewardValue *int64
	Message     string
}

type AnnouncementItem struct {
	Source      string
	Content     string
	Kind        string
	Extra       *string
	PublishedAt *time.Time
}

type AnnouncementResult struct {
	Items    []AnnouncementItem
	Warnings []string
}
