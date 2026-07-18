export type AdapterName = 'new-api' | 'sub2api' | 'zen-api';
export type AdapterSelection = AdapterName | 'auto';
export type Capabilities = { checkin: boolean; announcements: boolean; requiresUserId: boolean };
export type AdapterDescriptor = { name: AdapterName; displayName: string; capabilities: Capabilities };

export type Site = {
  id: string; name: string; baseUrl: string; adapter: AdapterName; userId: string;
  enabled: boolean; checkinEnabled: boolean; announcementEnabled: boolean;
  checkinCron: string; announcementCron: string; timezone: string;
  credentialConfigured: boolean; consecutiveFailures: number; capabilities: Capabilities;
  createdAt: string; updatedAt: string;
};

export type SiteWrite = {
  name: string; baseUrl: string; adapter: AdapterSelection; userId: string; accessToken?: string;
  enabled: boolean; checkinEnabled?: boolean; announcementEnabled?: boolean;
  checkinCron: string; announcementCron: string; timezone: string;
};

export type CheckinStatus = 'running' | 'success' | 'already_checked' | 'manual_required' | 'failed' | 'skipped';
export type CheckinRun = { id: string; siteId: string; siteName?: string; localDate: string; status: CheckinStatus; rewardValue: number | null; message: string; errorCode: string | null; attemptCount: number; startedAt: string; finishedAt: string | null; requestId: string };
export type Announcement = { id: string; siteId: string; siteName?: string; source: 'status' | 'notice'; fingerprint: string; content: string; kind: string; extra: string | null; publishedAt: string | null; firstSeenAt: string; lastSeenAt: string; readAt: string | null };
export type AnnouncementSync = { id: string; siteId: string; status: 'running' | 'success' | 'partial' | 'failed'; addedCount: number; message: string; startedAt: string; finishedAt: string | null; requestId: string };
export type Summary = { sites: { total: number; enabled: number }; today: Partial<Record<CheckinStatus, number>>; unreadAnnouncements: number };
export type APIErrorBody = { error: { code: string; message: string; retryable: boolean; requestId: string } };
