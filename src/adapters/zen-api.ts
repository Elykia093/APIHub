import type { SafeHttpClient } from '../security/safe-http.js';
import { bearerHeaders, parseGenericCheckinResponse } from './shared.js';
import type { AdapterCheckinResult, AdapterSiteContext, SiteAdapter } from './types.js';

export class ZenApiAdapter implements SiteAdapter {
  readonly name = 'zen-api' as const;
  readonly displayName = 'ZenAPI';
  readonly capabilities = {
    checkin: true,
    announcements: false,
    requiresUserId: false,
  } as const;

  constructor(private readonly http: SafeHttpClient) {}

  async checkIn(site: AdapterSiteContext): Promise<AdapterCheckinResult> {
    // Source: api-auto-chekin0624.zip uses Bearer auth with POST /api/u/checkin.
    const response = await this.http.requestJson({
      baseUrl: site.baseUrl,
      path: '/api/u/checkin',
      method: 'POST',
      headers: bearerHeaders(site.accessToken),
      body: {},
    });
    return parseGenericCheckinResponse(response);
  }
}
