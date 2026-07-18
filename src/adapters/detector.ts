import type { SiteAdapterName } from '../domain/types.js';
import { AppError } from '../lib/errors.js';
import type { HttpJsonResponse, SafeHttpClient } from '../security/safe-http.js';
import { isRecord } from './shared.js';

function readPayload(response: HttpJsonResponse): Record<string, unknown> | null {
  return isRecord(response.json) ? response.json : null;
}

function looksLikeZenApi(response: HttpJsonResponse): boolean {
  const payload = readPayload(response);
  if (!payload) return false;
  const data = isRecord(payload.data) ? payload.data : payload;
  return ['site_mode', 'registration_mode', 'linuxdo_enabled']
    .some((key) => Object.prototype.hasOwnProperty.call(data, key));
}

function looksLikeSub2Api(response: HttpJsonResponse): boolean {
  const payload = readPayload(response);
  if (!payload) return false;
  return Object.prototype.hasOwnProperty.call(payload, 'code')
    && (Object.prototype.hasOwnProperty.call(payload, 'message')
      || Object.prototype.hasOwnProperty.call(payload, 'data'));
}

function looksLikeNewApi(response: HttpJsonResponse): boolean {
  const payload = readPayload(response);
  if (!payload) return false;
  return typeof payload.success === 'boolean' || isRecord(payload.data);
}

export class SiteAdapterDetector {
  constructor(private readonly http: SafeHttpClient) {}

  async detect(baseUrl: string): Promise<SiteAdapterName> {
    const [zen, sub2, newApi] = await Promise.allSettled([
      this.http.requestJson({ baseUrl, path: '/api/public/site-info' }),
      this.http.requestJson({ baseUrl, path: '/api/v1/auth/me' }),
      this.http.requestJson({ baseUrl, path: '/api/status' }),
    ]);

    if (zen.status === 'fulfilled' && looksLikeZenApi(zen.value)) return 'zen-api';
    if (sub2.status === 'fulfilled' && looksLikeSub2Api(sub2.value)) return 'sub2api';
    if (newApi.status === 'fulfilled' && looksLikeNewApi(newApi.value)) return 'new-api';

    const failures = [zen, sub2, newApi].filter((result) => result.status === 'rejected');
    if (failures.length === 3 && failures[0]?.status === 'rejected') throw failures[0].reason;
    throw new AppError(
      422,
      'VALIDATION_ERROR',
      'Site type could not be detected; select a concrete adapter',
    );
  }
}
