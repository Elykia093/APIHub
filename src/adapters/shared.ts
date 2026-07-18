import { AppError } from '../lib/errors.js';
import type { HttpJsonResponse } from '../security/safe-http.js';
import type { AdapterCheckinResult } from './types.js';

export type JsonRecord = Record<string, unknown>;

export function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function readResponseMessage(response: HttpJsonResponse): string {
  if (isRecord(response.json)) {
    for (const key of ['message', 'msg', 'detail', 'data']) {
      const value = response.json[key];
      if (typeof value === 'string' && value.trim()) return value.trim().slice(0, 300);
    }
  }
  return response.text.trim().slice(0, 300) || `HTTP ${response.status}`;
}

export function bearerHeaders(accessToken: string): Record<string, string> {
  const value = /^Bearer\s+/i.test(accessToken) ? accessToken : `Bearer ${accessToken}`;
  return {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    Authorization: value,
  };
}

export function parseGenericCheckinResponse(response: HttpJsonResponse): AdapterCheckinResult {
  const payload = isRecord(response.json) ? response.json : {};
  const data = isRecord(payload.data) ? payload.data : {};
  const message = readResponseMessage(response);
  const alreadyChecked = payload.already_checked_in === true
    || data.already_checked_in === true
    || data.checked_in === true
    || /already\s*(checked|check(?:ed)?\s*in)|今日已签到|已经签到|重复签到|已签过/i.test(message);
  const manualRequired = /turnstile|captcha|验证码|人机验证|二次验证|manual/i.test(message);
  const success = response.ok && (
    payload.success === true
    || payload.status === 'success'
    || payload.ret === 1
    || payload.code === 0
    || payload.ok === true
    || data.success === true
  );

  if (alreadyChecked) {
    return { status: 'already_checked', rewardValue: null, message };
  }
  if (manualRequired) {
    return { status: 'manual_required', rewardValue: null, message };
  }
  if (!success) {
    throw new AppError(
      502,
      'UPSTREAM_REJECTED',
      `Check-in was rejected: ${message}`,
      response.status >= 500,
    );
  }

  const rawReward = data.reward ?? data.quota_awarded ?? payload.reward;
  const rewardValue = typeof rawReward === 'number' && Number.isSafeInteger(rawReward)
    ? rawReward
    : null;
  return { status: 'success', rewardValue, message: message || 'Check-in succeeded' };
}

export function normalizePublishedAt(value: unknown): string | null {
  if (typeof value === 'number' && Number.isFinite(value)) {
    const millis = value > 10_000_000_000 ? value : value * 1_000;
    return new Date(millis).toISOString();
  }
  if (typeof value !== 'string' || !value.trim()) return null;
  const numeric = Number(value);
  if (Number.isFinite(numeric)) return normalizePublishedAt(numeric);
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : null;
}
