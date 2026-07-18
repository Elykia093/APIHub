import { clearSession, sessionToken } from './session';
import type { AdapterDescriptor, Announcement, AnnouncementSync, APIErrorBody, CheckinRun, Site, SiteWrite, Summary } from './types';

export class APIError extends Error {
  constructor(readonly status: number, readonly code: string, message: string, readonly retryable: boolean, readonly requestId: string) { super(message); this.name = 'APIError'; }
}

type RequestOptions = Omit<RequestInit, 'signal'> & { signal?: AbortSignal; token?: string };
const withSignal = (signal?: AbortSignal): Pick<RequestOptions, 'signal'> => signal ? { signal } : {};

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(new DOMException('Request timed out', 'TimeoutError')), 10_000);
  const abort = () => controller.abort(options.signal?.reason);
  options.signal?.addEventListener('abort', abort, { once: true });
  const headers = new Headers(options.headers);
  const token = options.token ?? sessionToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  if (options.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  try {
    const response = await fetch(path, { ...options, headers, signal: controller.signal });
    const payload = await response.json().catch((): Record<string, never> => ({}));
    if (!response.ok) {
      const body = payload as Partial<APIErrorBody>;
      if (response.status === 401) { clearSession(); window.dispatchEvent(new Event('apihub:unauthorized')); }
      throw new APIError(response.status, body.error?.code ?? 'HTTP_ERROR', body.error?.message ?? `HTTP ${response.status}`, body.error?.retryable ?? false, body.error?.requestId ?? '');
    }
    return payload as T;
  } catch (error) {
    if (error instanceof APIError) throw error;
    if (controller.signal.aborted && !options.signal?.aborted) throw new APIError(0, 'UPSTREAM_TIMEOUT', '请求超时，请稍后重试', true, '');
    if (options.signal?.aborted) throw error;
    throw new APIError(0, 'NETWORK_ERROR', '无法连接服务器，请检查网络', true, '');
  } finally {
    window.clearTimeout(timeout);
    options.signal?.removeEventListener('abort', abort);
  }
}

export const api = {
  validateToken: (token: string, signal?: AbortSignal) => request<Summary>('/api/v1/summary', { token, ...withSignal(signal) }),
  summary: (signal?: AbortSignal) => request<Summary>('/api/v1/summary', withSignal(signal)),
  adapters: async (signal?: AbortSignal) => (await request<{ data: AdapterDescriptor[] }>('/api/v1/site-adapters', withSignal(signal))).data,
  sites: async (signal?: AbortSignal) => (await request<{ data: Site[] }>('/api/v1/sites', withSignal(signal))).data,
  site: async (id: string, signal?: AbortSignal) => (await request<{ data: Site }>(`/api/v1/sites/${id}`, withSignal(signal))).data,
  createSite: async (input: SiteWrite) => (await request<{ data: Site }>('/api/v1/sites', { method: 'POST', body: JSON.stringify(input) })).data,
  updateSite: async (id: string, input: Partial<SiteWrite>) => (await request<{ data: Site }>(`/api/v1/sites/${id}`, { method: 'PATCH', body: JSON.stringify(input) })).data,
  checkins: async (signal?: AbortSignal) => (await request<{ data: CheckinRun[] }>('/api/v1/checkin-runs?limit=100', withSignal(signal))).data,
  runCheckin: async (siteId: string) => (await request<{ data: CheckinRun }>(`/api/v1/sites/${siteId}/checkin-runs`, { method: 'POST' })).data,
  announcements: async (signal?: AbortSignal) => (await request<{ data: Announcement[] }>('/api/v1/announcements?limit=100', withSignal(signal))).data,
  syncAnnouncements: async (siteId: string) => (await request<{ data: AnnouncementSync }>(`/api/v1/sites/${siteId}/announcement-syncs`, { method: 'POST' })).data,
  setAnnouncementRead: async (id: string, read: boolean) => (await request<{ data: Announcement }>(`/api/v1/announcements/${id}`, { method: 'PATCH', body: JSON.stringify({ read }) })).data,
};
