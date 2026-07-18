import { lookup as dnsLookup } from 'node:dns';
import type { LookupFunction } from 'node:net';
import { Agent, fetch } from 'undici';
import ipaddr from 'ipaddr.js';
import { AppError } from '../lib/errors.js';

export type HttpJsonResponse = {
  status: number;
  ok: boolean;
  text: string;
  json: unknown;
};

export type SafeHttpOptions = {
  timeoutMs: number;
  maxResponseBytes: number;
  allowPrivateSites: boolean;
  allowInsecureHttp: boolean;
};

function isPublicAddress(address: string): boolean {
  let parsed = ipaddr.parse(address);
  if (parsed.kind() === 'ipv6') {
    const ipv6 = parsed as ipaddr.IPv6;
    if (ipv6.isIPv4MappedAddress()) parsed = ipv6.toIPv4Address();
  }
  return parsed.range() === 'unicast';
}

function assertLiteralAddressAllowed(hostname: string, allowPrivateSites: boolean): void {
  if (allowPrivateSites) return;
  const address = hostname.startsWith('[') && hostname.endsWith(']')
    ? hostname.slice(1, -1)
    : hostname;
  if (ipaddr.isValid(address) && !isPublicAddress(address)) {
    throw new AppError(422, 'SITE_URL_BLOCKED', 'Private or reserved IP addresses are not allowed');
  }
}

function createLookup(allowPrivateSites: boolean): LookupFunction {
  return (hostname, options, callback) => {
    dnsLookup(hostname, { all: true, verbatim: true }, (error, addresses) => {
      if (error) {
        callback(error, '', 0);
        return;
      }
      if (addresses.length === 0) {
        callback(new Error('Hostname did not resolve'), '', 0);
        return;
      }
      if (!allowPrivateSites && addresses.some((entry) => !isPublicAddress(entry.address))) {
        callback(new AppError(422, 'SITE_URL_BLOCKED', 'Site resolves to a private or reserved address'), '', 0);
        return;
      }

      const wantsAll = Boolean(options.all);
      if (wantsAll) {
        callback(null, addresses);
        return;
      }
      const first = addresses[0]!;
      callback(null, first.address, first.family);
    });
  };
}

export function normalizeBaseUrl(
  input: string,
  allowInsecureHttp: boolean,
  allowPrivateSites = false,
): string {
  let parsed: URL;
  try {
    parsed = new URL(input);
  } catch {
    throw new AppError(422, 'VALIDATION_ERROR', 'baseUrl must be a valid absolute URL');
  }
  if (parsed.username || parsed.password) {
    throw new AppError(422, 'VALIDATION_ERROR', 'baseUrl must not include credentials');
  }
  if (parsed.protocol !== 'https:' && !(allowInsecureHttp && parsed.protocol === 'http:')) {
    throw new AppError(422, 'SITE_URL_BLOCKED', 'Only HTTPS site URLs are allowed');
  }
  assertLiteralAddressAllowed(parsed.hostname, allowPrivateSites);
  parsed.search = '';
  parsed.hash = '';
  parsed.pathname = parsed.pathname.replace(/\/+$/, '');
  return parsed.toString().replace(/\/$/, '');
}

export function joinSiteUrl(baseUrl: string, path: string): string {
  const prefix = baseUrl.endsWith('/') ? baseUrl : `${baseUrl}/`;
  return new URL(path.replace(/^\/+/, ''), prefix).toString();
}

async function readLimitedBody(response: Response, maxBytes: number): Promise<string> {
  const length = Number(response.headers.get('content-length') || 0);
  if (Number.isFinite(length) && length > maxBytes) {
    await response.body?.cancel();
    throw new AppError(502, 'UPSTREAM_RESPONSE_TOO_LARGE', 'Upstream response exceeded the configured limit');
  }
  if (!response.body) return '';

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxBytes) {
      await reader.cancel();
      throw new AppError(502, 'UPSTREAM_RESPONSE_TOO_LARGE', 'Upstream response exceeded the configured limit');
    }
    chunks.push(value);
  }
  return Buffer.concat(chunks.map((chunk) => Buffer.from(chunk))).toString('utf8');
}

function findAppError(error: unknown): AppError | null {
  const seen = new Set<unknown>();
  let current = error;
  for (let depth = 0; depth < 8; depth += 1) {
    if (current instanceof AppError) return current;
    if (typeof current !== 'object' || current === null || seen.has(current) || !('cause' in current)) return null;
    seen.add(current);
    current = (current as { cause?: unknown }).cause;
  }
  return null;
}

export class SafeHttpClient {
  readonly #options: SafeHttpOptions;
  readonly #agent: Agent;

  constructor(options: SafeHttpOptions) {
    this.#options = options;
    this.#agent = new Agent({
      connect: {
        lookup: createLookup(options.allowPrivateSites),
      },
    });
  }

  async requestJson(input: {
    baseUrl: string;
    path: string;
    method?: 'GET' | 'POST';
    headers?: Record<string, string>;
    body?: unknown;
  }): Promise<HttpJsonResponse> {
    const baseUrl = normalizeBaseUrl(
      input.baseUrl,
      this.#options.allowInsecureHttp,
      this.#options.allowPrivateSites,
    );
    const url = joinSiteUrl(baseUrl, input.path);
    try {
      const requestInit = {
        method: input.method ?? 'GET',
        redirect: 'manual',
        signal: AbortSignal.timeout(this.#options.timeoutMs),
        dispatcher: this.#agent,
        ...(input.headers ? { headers: input.headers } : {}),
        ...(input.body === undefined ? {} : { body: JSON.stringify(input.body) }),
      } as const;
      const response = await fetch(url, requestInit);
      if (response.status >= 300 && response.status < 400) {
        await response.body?.cancel();
        throw new AppError(502, 'UPSTREAM_REDIRECT_BLOCKED', 'Upstream redirect was blocked');
      }
      const text = await readLimitedBody(response as unknown as Response, this.#options.maxResponseBytes);
      let json: unknown = null;
      if (text.trim()) {
        try {
          json = JSON.parse(text);
        } catch {
          json = null;
        }
      }
      return { status: response.status, ok: response.ok, text, json };
    } catch (error) {
      const appError = findAppError(error);
      if (appError) throw appError;
      const name = error instanceof Error ? error.name : '';
      if (name === 'TimeoutError' || name === 'AbortError') {
        throw new AppError(504, 'UPSTREAM_TIMEOUT', 'Upstream request timed out', true, { cause: error });
      }
      throw new AppError(502, 'UPSTREAM_REJECTED', 'Unable to reach upstream site', true, { cause: error });
    }
  }

  async close(): Promise<void> {
    await this.#agent.close();
  }
}
