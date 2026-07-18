export const SITE_ADAPTER_NAMES = ['new-api', 'sub2api', 'zen-api'] as const;

export type SiteAdapterName = (typeof SITE_ADAPTER_NAMES)[number];
export type SiteAdapterSelection = SiteAdapterName | 'auto';

export type SiteAdapterCapabilities = {
  checkin: boolean;
  announcements: boolean;
  requiresUserId: boolean;
};

export type SiteRecord = {
  id: string;
  name: string;
  baseUrl: string;
  adapter: SiteAdapterName;
  userId: string;
  accessTokenCiphertext: string;
  enabled: boolean;
  checkinEnabled: boolean;
  announcementEnabled: boolean;
  checkinCron: string;
  announcementCron: string;
  timezone: string;
  consecutiveFailures: number;
  createdAt: string;
  updatedAt: string;
};

export type PublicSite = Omit<SiteRecord, 'accessTokenCiphertext'> & {
  credentialConfigured: boolean;
  capabilities: SiteAdapterCapabilities;
};

export type CheckinStatus =
  | 'running'
  | 'success'
  | 'already_checked'
  | 'manual_required'
  | 'failed'
  | 'skipped';

export type CheckinRun = {
  id: string;
  siteId: string;
  siteName?: string;
  localDate: string;
  status: CheckinStatus;
  rewardValue: number | null;
  message: string;
  errorCode: string | null;
  attemptCount: number;
  startedAt: string;
  finishedAt: string | null;
  requestId: string;
};

export type AnnouncementSource = 'status' | 'notice';

export type Announcement = {
  id: string;
  siteId: string;
  siteName?: string;
  source: AnnouncementSource;
  fingerprint: string;
  content: string;
  kind: string;
  extra: string | null;
  publishedAt: string | null;
  firstSeenAt: string;
  lastSeenAt: string;
  readAt: string | null;
};

export type AnnouncementSyncRun = {
  id: string;
  siteId: string;
  status: 'running' | 'success' | 'partial' | 'failed';
  addedCount: number;
  message: string;
  startedAt: string;
  finishedAt: string | null;
  requestId: string;
};

export function toPublicSite(site: SiteRecord, capabilities: SiteAdapterCapabilities): PublicSite {
  const { accessTokenCiphertext, ...publicFields } = site;
  return {
    ...publicFields,
    credentialConfigured: accessTokenCiphertext.length > 0,
    capabilities,
  };
}
