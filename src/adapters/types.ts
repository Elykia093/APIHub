import type {
  AnnouncementSource,
  SiteAdapterCapabilities,
  SiteAdapterName,
} from '../domain/types.js';

export type AdapterSiteContext = {
  baseUrl: string;
  userId: string;
  accessToken: string;
  timezone: string;
};

export type AdapterCheckinResult = {
  status: 'success' | 'already_checked' | 'manual_required';
  rewardValue: number | null;
  message: string;
};

export type AdapterAnnouncement = {
  source: AnnouncementSource;
  content: string;
  kind: string;
  extra: string | null;
  publishedAt: string | null;
};

export type AdapterAnnouncementResult = {
  items: AdapterAnnouncement[];
  warnings: string[];
};

export interface SiteAdapter {
  readonly name: SiteAdapterName;
  readonly displayName: string;
  readonly capabilities: SiteAdapterCapabilities;
  checkIn?(site: AdapterSiteContext, localDate: string): Promise<AdapterCheckinResult>;
  fetchAnnouncements?(site: AdapterSiteContext): Promise<AdapterAnnouncementResult>;
}
