import { AppError } from '../lib/errors.js';
import type { SafeHttpClient, HttpJsonResponse } from '../security/safe-http.js';
import type {
  AdapterAnnouncement,
  AdapterAnnouncementResult,
  AdapterCheckinResult,
  AdapterSiteContext,
  SiteAdapter,
} from './types.js';

type JsonRecord = Record<string, unknown>;

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readMessage(response: HttpJsonResponse): string {
  if (isRecord(response.json) && typeof response.json.message === 'string') {
    return response.json.message.trim();
  }
  return response.text.trim().slice(0, 300) || `HTTP ${response.status}`;
}

function isSuccessPayload(value: unknown): value is JsonRecord {
  return isRecord(value) && value.success === true;
}

function looksAlreadyChecked(message: string): boolean {
  return /already\s*(checked|check(?:ed)?\s*in)|今日已签到|已经签到|重复签到/i.test(message);
}

function looksManualRequired(message: string): boolean {
  return /turnstile|captcha|验证码|人机验证|二次验证|manual/i.test(message);
}

function normalizePublishedAt(value: unknown): string | null {
  if (typeof value !== 'string' || !value.trim()) return null;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : null;
}

function authHeaders(site: AdapterSiteContext): Record<string, string> {
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    Authorization: site.accessToken,
    'New-Api-User': site.userId,
  };
}

export class NewApiAdapter implements SiteAdapter {
  readonly name = 'new-api' as const;
  readonly displayName = 'New API';
  readonly capabilities = {
    checkin: true,
    announcements: true,
    requiresUserId: true,
  } as const;

  constructor(private readonly http: SafeHttpClient) {}

  async checkIn(site: AdapterSiteContext, localDate: string): Promise<AdapterCheckinResult> {
    const publicStatus = await this.http.requestJson({ baseUrl: site.baseUrl, path: '/api/status' });
    if (publicStatus.ok && isSuccessPayload(publicStatus.json)) {
      const statusData = isRecord(publicStatus.json.data) ? publicStatus.json.data : {};
      if (statusData.checkin_enabled === false) {
        throw new AppError(502, 'UPSTREAM_REJECTED', 'Upstream site has disabled check-in');
      }
      if (statusData.turnstile_check === true) {
        return {
          status: 'manual_required',
          rewardValue: null,
          message: 'Upstream site requires Turnstile or CAPTCHA verification',
        };
      }
    }

    const month = localDate.slice(0, 7);
    const status = await this.http.requestJson({
      baseUrl: site.baseUrl,
      path: `/api/user/checkin?month=${encodeURIComponent(month)}`,
      headers: authHeaders(site),
    });
    if (!status.ok || !isSuccessPayload(status.json)) {
      const message = readMessage(status);
      if (looksAlreadyChecked(message)) {
        return { status: 'already_checked', rewardValue: null, message };
      }
      if (looksManualRequired(message)) {
        return { status: 'manual_required', rewardValue: null, message };
      }
      throw new AppError(502, 'UPSTREAM_REJECTED', `Unable to read check-in status: ${message}`, status.status >= 500);
    }

    const data = isRecord(status.json.data) ? status.json.data : {};
    const stats = isRecord(data.stats) ? data.stats : {};
    if (stats.checked_in_today === true) {
      return { status: 'already_checked', rewardValue: null, message: 'Already checked in today' };
    }

    const result = await this.http.requestJson({
      baseUrl: site.baseUrl,
      path: '/api/user/checkin',
      method: 'POST',
      headers: authHeaders(site),
      body: {},
    });
    const message = readMessage(result);
    if (!result.ok || !isSuccessPayload(result.json)) {
      if (looksAlreadyChecked(message)) {
        return { status: 'already_checked', rewardValue: null, message };
      }
      if (looksManualRequired(message)) {
        return { status: 'manual_required', rewardValue: null, message };
      }
      throw new AppError(502, 'UPSTREAM_REJECTED', `Check-in was rejected: ${message}`, result.status >= 500);
    }
    const resultData = isRecord(result.json.data) ? result.json.data : {};
    const reward = typeof resultData.quota_awarded === 'number' && Number.isSafeInteger(resultData.quota_awarded)
      ? resultData.quota_awarded
      : null;
    return {
      status: 'success',
      rewardValue: reward,
      message: message || 'Check-in succeeded',
    };
  }

  async fetchAnnouncements(site: AdapterSiteContext): Promise<AdapterAnnouncementResult> {
    const [statusResult, noticeResult] = await Promise.allSettled([
      this.http.requestJson({ baseUrl: site.baseUrl, path: '/api/status' }),
      this.http.requestJson({ baseUrl: site.baseUrl, path: '/api/notice' }),
    ]);

    const items: AdapterAnnouncement[] = [];
    const warnings: string[] = [];

    if (statusResult.status === 'fulfilled' && statusResult.value.ok && isSuccessPayload(statusResult.value.json)) {
      const data = isRecord(statusResult.value.json.data) ? statusResult.value.json.data : {};
      if (data.announcements_enabled !== false && Array.isArray(data.announcements)) {
        for (const candidate of data.announcements.slice(0, 100)) {
          if (!isRecord(candidate) || typeof candidate.content !== 'string' || !candidate.content.trim()) continue;
          items.push({
            source: 'status',
            content: candidate.content.trim(),
            kind: typeof candidate.type === 'string' ? candidate.type.slice(0, 32) : 'default',
            extra: typeof candidate.extra === 'string' && candidate.extra.trim() ? candidate.extra.trim() : null,
            publishedAt: normalizePublishedAt(candidate.publishDate),
          });
        }
      }
    } else {
      warnings.push('Structured announcements could not be fetched');
    }

    if (noticeResult.status === 'fulfilled' && noticeResult.value.ok && isSuccessPayload(noticeResult.value.json)) {
      const notice = typeof noticeResult.value.json.data === 'string' ? noticeResult.value.json.data.trim() : '';
      if (notice) {
        items.push({ source: 'notice', content: notice, kind: 'default', extra: null, publishedAt: null });
      }
    } else {
      warnings.push('Text notice could not be fetched');
    }

    if (warnings.length === 2) {
      throw new AppError(502, 'UPSTREAM_REJECTED', 'All announcement sources failed', true);
    }
    return { items, warnings };
  }
}
