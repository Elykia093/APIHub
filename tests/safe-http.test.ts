import { once } from 'node:events';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { describe, expect, it } from 'vitest';
import { AppError } from '../src/lib/errors.js';
import { joinSiteUrl, normalizeBaseUrl, SafeHttpClient } from '../src/security/safe-http.js';

describe('safe HTTP URL handling', () => {
  it('requires HTTPS unless insecure HTTP is explicitly enabled', () => {
    expect(() => normalizeBaseUrl('http://example.com', false)).toThrowError(AppError);
    expect(normalizeBaseUrl('http://example.com/path/', true)).toBe('http://example.com/path');
  });

  it('blocks private literal IPv4 and IPv6 addresses before any network request', () => {
    expect(() => normalizeBaseUrl('https://127.0.0.1', false)).toThrowError(AppError);
    expect(() => normalizeBaseUrl('https://[::1]', false)).toThrowError(AppError);
    expect(normalizeBaseUrl('http://127.0.0.1/path', true, true)).toBe('http://127.0.0.1/path');
  });

  it('rejects embedded credentials and strips query fragments from base URLs', () => {
    expect(() => normalizeBaseUrl('https://user:password@example.com', false)).toThrowError(AppError);
    expect(normalizeBaseUrl('https://example.com/base/?secret=1#fragment', false)).toBe('https://example.com/base');
  });

  it('joins API paths under an optional base path', () => {
    expect(joinSiteUrl('https://example.com/base', '/api/status')).toBe('https://example.com/base/api/status');
  });

  it('pins WHATWG host and path canonicalization', () => {
    expect(normalizeBaseUrl('HTTPS://EXAMPLE.COM:443/a/../b/', false, true)).toBe('https://example.com/b');
    expect(normalizeBaseUrl('https://example.com:444/a//b/', false, true)).toBe('https://example.com:444/a//b');
    expect(normalizeBaseUrl('https://bücher.example/path', false, true)).toBe('https://xn--bcher-kva.example/path');
    expect(normalizeBaseUrl('https://example.com/%7euser/', false, true)).toBe('https://example.com/%7euser');
  });

  it('blocks private DNS results, redirects, and oversized upstream responses', async () => {
    const server = createServer((request, reply) => {
      if (request.url === '/slow') {
        setTimeout(() => sendLargeResponse(reply), 150);
        return;
      }
      if (request.url === '/redirect') {
        reply.writeHead(302, { location: '/target' });
        reply.end();
        return;
      }
      sendLargeResponse(reply);
    });
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const address = server.address() as AddressInfo;
    const localhostBase = `http://localhost:${address.port}`;
    const literalBase = `http://127.0.0.1:${address.port}`;
    const blockedClient = new SafeHttpClient({
      timeoutMs: 1_000,
      maxResponseBytes: 32,
      allowPrivateSites: false,
      allowInsecureHttp: true,
    });
    const allowedClient = new SafeHttpClient({
      timeoutMs: 1_000,
      maxResponseBytes: 32,
      allowPrivateSites: true,
      allowInsecureHttp: true,
    });
    const timeoutClient = new SafeHttpClient({
      timeoutMs: 25,
      maxResponseBytes: 1_024,
      allowPrivateSites: true,
      allowInsecureHttp: true,
    });

    try {
      await expect(blockedClient.requestJson({ baseUrl: localhostBase, path: '/large' }))
        .rejects.toMatchObject({ code: 'SITE_URL_BLOCKED' });
      await expect(allowedClient.requestJson({ baseUrl: literalBase, path: '/redirect' }))
        .rejects.toMatchObject({ code: 'UPSTREAM_REDIRECT_BLOCKED' });
      await expect(allowedClient.requestJson({ baseUrl: literalBase, path: '/large' }))
        .rejects.toMatchObject({ code: 'UPSTREAM_RESPONSE_TOO_LARGE' });
      await expect(timeoutClient.requestJson({ baseUrl: literalBase, path: '/slow' }))
        .rejects.toMatchObject({ code: 'UPSTREAM_TIMEOUT', retryable: true });
    } finally {
      await blockedClient.close();
      await allowedClient.close();
      await timeoutClient.close();
      server.close();
      await once(server, 'close');
    }
  });
});

function sendLargeResponse(reply: import('node:http').ServerResponse): void {
  reply.writeHead(200, { 'content-type': 'application/json' });
  reply.end(JSON.stringify({ data: 'x'.repeat(128) }));
}
