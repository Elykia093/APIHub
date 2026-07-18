import { AppError } from '../lib/errors.js';
import type { SafeHttpClient } from '../security/safe-http.js';
import {
  bearerHeaders,
  isRecord,
  normalizePublishedAt,
  readResponseMessage,
} from './shared.js';
import type {
  AdapterAnnouncement,
  AdapterAnnouncementResult,
  AdapterSiteContext,
  SiteAdapter,
} from './types.js';

function readAnnouncementItems(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  if (!isRecord(value)) return [];
  return Array.isArray(value.items) ? value.items : [];
}

export class Sub2ApiAdapter implements SiteAdapter {
  readonly name = 'sub2api' as const;
  readonly displayName = 'Sub2API';
  readonly capabilities = {
    checkin: false,
    announcements: true,
    requiresUserId: false,
  } as const;

  constructor(private readonly http: SafeHttpClient) {}

  async fetchAnnouncements(site: AdapterSiteContext): Promise<AdapterAnnouncementResult> {
    // Source: qixing-jk/all-api-hub and Wei-Shaw/sub2api use this JWT-protected route.
    const response = await this.http.requestJson({
      baseUrl: site.baseUrl,
      path: '/api/v1/announcements?unread_only=1',
      headers: bearerHeaders(site.accessToken),
    });
    const payload = isRecord(response.json) ? response.json : {};
    if (!response.ok || (payload.code !== undefined && payload.code !== 0)) {
      throw new AppError(
        502,
        'UPSTREAM_REJECTED',
        `Unable to fetch Sub2API announcements: ${readResponseMessage(response)}`,
        response.status >= 500,
      );
    }

    const items: AdapterAnnouncement[] = [];
    for (const candidate of readAnnouncementItems(payload.data).slice(0, 100)) {
      if (!isRecord(candidate)) continue;
      const content = [candidate.content, candidate.message, candidate.body]
        .find((value) => typeof value === 'string' && value.trim());
      const title = typeof candidate.title === 'string' ? candidate.title.trim() : '';
      if (typeof content !== 'string' && !title) continue;
      items.push({
        source: 'notice',
        content: typeof content === 'string' ? content.trim() : title,
        kind: 'account',
        extra: title && title !== content ? title : null,
        publishedAt: normalizePublishedAt(candidate.updated_at ?? candidate.created_at),
      });
    }
    return { items, warnings: [] };
  }
}
