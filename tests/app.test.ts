import { randomUUID } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { ADMIN_TOKEN, authHeaders, buildTestApp, siteBody } from './helpers.js';

describe('HTTP application', () => {
  it('serves the dashboard and its static assets', async () => {
    const runtime = await buildTestApp();
    try {
      const index = await runtime.app.inject({ method: 'GET', url: '/' });
      const script = await runtime.app.inject({ method: 'GET', url: '/app.js' });
      const styles = await runtime.app.inject({ method: 'GET', url: '/styles.css' });

      expect(index.statusCode).toBe(200);
      expect(index.headers['content-type']).toContain('text/html');
      expect(index.body).toContain('<title>APIHub</title>');
      expect(index.body).toContain('value="sub2api"');
      expect(script.statusCode).toBe(200);
      expect(script.headers['content-type']).toContain('javascript');
      expect(styles.statusCode).toBe(200);
      expect(styles.headers['content-type']).toContain('text/css');
    } finally {
      await runtime.app.close();
    }
  });

  it('requires administrator authentication for every management API', async () => {
    const runtime = await buildTestApp();
    try {
      const missing = await runtime.app.inject({ method: 'GET', url: '/api/v1/sites' });
      const wrong = await runtime.app.inject({
        method: 'GET',
        url: '/api/v1/sites',
        headers: { authorization: 'Bearer wrong-token' },
      });
      const trailingSpace = await runtime.app.inject({
        method: 'GET',
        url: '/api/v1/sites',
        headers: { authorization: `Bearer ${ADMIN_TOKEN} ` },
      });

      expect(missing.statusCode).toBe(401);
      expect(missing.json().error.code).toBe('AUTH_REQUIRED');
      expect(missing.headers['cache-control']).toBe('no-store');
      expect(wrong.statusCode).toBe(401);
      expect(trailingSpace.statusCode).toBe(401);

      const hiddenUnknown = await runtime.app.inject({ method: 'GET', url: '/api/v1/not-a-route' });
      expect(hiddenUnknown.statusCode).toBe(401);
      const visibleUnknown = await runtime.app.inject({
        method: 'GET',
        url: '/api/v1/not-a-route',
        headers: authHeaders(),
      });
      expect(visibleUnknown.statusCode).toBe(404);
      expect(visibleUnknown.json().error.code).toBe('NOT_FOUND');
      expect(visibleUnknown.headers['cache-control']).toBe('no-store');
    } finally {
      await runtime.app.close();
    }
  });

  it('keeps HEAD and trailing-slash routing semantics stable', async () => {
    const runtime = await buildTestApp();
    try {
      for (const url of ['/api/v1/sites', '/api/v1/site-adapters', '/health/live', '/health/ready']) {
        const response = await runtime.app.inject({
          method: 'HEAD',
          url,
          ...(url.startsWith('/api/') ? { headers: authHeaders() } : {}),
        });
        expect(response.statusCode).toBe(200);
      }

      const cases = [
        { method: 'POST' as const, url: '/api/v1/sites/', status: 404 },
        { method: 'GET' as const, url: '/api/v1/sites/', status: 422 },
        { method: 'PATCH' as const, url: `/api/v1/announcements/${randomUUID()}/`, status: 404 },
        { method: 'GET' as const, url: '/health/live/', status: 404 },
      ];
      for (const test of cases) {
        const response = await runtime.app.inject({
          method: test.method,
          url: test.url,
          ...(test.url.startsWith('/api/') ? { headers: authHeaders() } : {}),
        });
        expect(response.statusCode).toBe(test.status);
        if (test.method === 'GET' && test.url === '/api/v1/sites/') {
          expect(response.json().error).toMatchObject({
            code: 'VALIDATION_ERROR',
            message: 'siteId: Invalid UUID',
          });
        }
      }
    } finally {
      await runtime.app.close();
    }
  });

  it('does not spend the global rate-limit budget before authentication', async () => {
    const runtime = await buildTestApp();
    try {
      for (let attempt = 0; attempt < 245; attempt += 1) {
        const response = await runtime.app.inject({ method: 'GET', url: '/api/v1/sites' });
        expect(response.statusCode).toBe(401);
      }
    } finally {
      await runtime.app.close();
    }
  });

  it('rejects unknown fields, never echoes credentials, and preserves PATCH false', async () => {
    const runtime = await buildTestApp();
    try {
      const invalid = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: authHeaders(),
        payload: siteBody({ unexpected: true }),
      });
      expect(invalid.statusCode).toBe(422);
      expect(invalid.json().error.code).toBe('VALIDATION_ERROR');

      for (const field of ['adapter', 'checkinCron', 'announcementCron', 'timezone']) {
        const empty = await runtime.app.inject({
          method: 'POST',
          url: '/api/v1/sites',
          headers: authHeaders(),
          payload: siteBody({ [field]: '' }),
        });
        expect(empty.statusCode).toBe(422);
        expect(empty.json().error.code).toBe('VALIDATION_ERROR');
      }

      const accessToken = 'do-not-echo-this-token';
      const created = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: authHeaders(),
        payload: siteBody({ accessToken }),
      });
      expect(created.statusCode).toBe(201);
      expect(created.body).not.toContain(accessToken);
      expect(created.body).not.toContain('accessTokenCiphertext');
      expect(created.json().data.credentialConfigured).toBe(true);

      const patched = await runtime.app.inject({
        method: 'PATCH',
        url: `/api/v1/sites/${created.json().data.id}`,
        headers: authHeaders(),
        payload: { enabled: false, checkinEnabled: false, announcementEnabled: false },
      });
      expect(patched.statusCode).toBe(200);
      expect(patched.json().data).toMatchObject({
        enabled: false,
        checkinEnabled: false,
        announcementEnabled: false,
      });
      expect(patched.body).not.toContain(accessToken);
    } finally {
      await runtime.app.close();
    }
  });

  it('maps malformed JSON, oversized bodies, and unsupported media types to stable errors', async () => {
    const runtime = await buildTestApp();
    try {
      const malformed = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: { ...authHeaders(), 'content-type': 'application/json' },
        payload: '{bad-json',
      });
      expect(malformed.statusCode).toBe(400);
      expect(malformed.json().error.code).toBe('BAD_REQUEST');

      const oversized = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: authHeaders(),
        payload: { name: 'x'.repeat(70 * 1024) },
      });
      expect(oversized.statusCode).toBe(413);
      expect(oversized.json().error.code).toBe('PAYLOAD_TOO_LARGE');

      const unsupported = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: { ...authHeaders(), 'content-type': 'application/xml' },
        payload: '<site />',
      });
      expect(unsupported.statusCode).toBe(415);
      expect(unsupported.json().error.code).toBe('UNSUPPORTED_MEDIA_TYPE');

      const invalidJsonMedia = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: { ...authHeaders(), 'content-type': 'application/jsonp' },
        payload: '{}',
      });
      expect(invalidJsonMedia.statusCode).toBe(415);
      expect(invalidJsonMedia.json().error.code).toBe('UNSUPPORTED_MEDIA_TYPE');
    } finally {
      await runtime.app.close();
    }
  });

  it('rejects empty and duplicate list query values', async () => {
    const runtime = await buildTestApp();
    try {
      for (const url of [
        '/api/v1/checkin-runs?limit=',
        '/api/v1/checkin-runs?limit=1&limit=2',
        '/api/v1/checkin-runs?siteId=',
      ]) {
        const response = await runtime.app.inject({ method: 'GET', url, headers: authHeaders() });
        expect(response.statusCode).toBe(422);
        expect(response.json().error.code).toBe('VALIDATION_ERROR');
      }
      for (const url of [
        '/api/v1/checkin-runs?limit=1e2',
        '/api/v1/checkin-runs?limit=1.0',
        '/api/v1/checkin-runs?limit=0x10',
        '/api/v1/checkin-runs?limit=0b10',
        '/api/v1/checkin-runs?limit=0o10',
      ]) {
        const response = await runtime.app.inject({ method: 'GET', url, headers: authHeaders() });
        expect(response.statusCode).toBe(200);
      }
    } finally {
      await runtime.app.close();
    }
  });

  it('rejects private literal station addresses when private access is disabled', async () => {
    const runtime = await buildTestApp({
      allowPrivateSites: false,
      allowInsecureHttp: false,
    });
    try {
      const response = await runtime.app.inject({
        method: 'POST',
        url: '/api/v1/sites',
        headers: authHeaders(),
        payload: siteBody({ baseUrl: 'https://127.0.0.1' }),
      });

      expect(response.statusCode).toBe(422);
      expect(response.json().error.code).toBe('SITE_URL_BLOCKED');
    } finally {
      await runtime.app.close();
    }
  });

  it('returns the stable rate-limit error after the route budget is exhausted', async () => {
    const runtime = await buildTestApp();
    try {
      const siteId = randomUUID();
      let response;
      for (let attempt = 0; attempt < 11; attempt += 1) {
        response = await runtime.app.inject({
          method: 'POST',
          url: `/api/v1/sites/${siteId}/checkin-runs`,
          headers: { authorization: `Bearer ${ADMIN_TOKEN}` },
        });
      }

      expect(response?.statusCode).toBe(429);
      expect(response?.json().error).toMatchObject({ code: 'RATE_LIMITED', retryable: true });
    } finally {
      await runtime.app.close();
    }
  });
});
