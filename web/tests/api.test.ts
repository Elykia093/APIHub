import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api';
import { sessionToken, setSession } from '@/session';

describe('API client', () => {
  beforeEach(() => { sessionStorage.clear(); window.location.hash = ''; });

  it('clears the tab-scoped token after a 401', async () => {
    setSession('test-admin-token-1234567890');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error:{ code:'AUTH_REQUIRED',message:'Administrator authentication required',retryable:false,requestId:'request-1' } }), { status:401, headers:{'Content-Type':'application/json'} })));
    await expect(api.summary()).rejects.toMatchObject({ status:401, code:'AUTH_REQUIRED' });
    expect(sessionToken()).toBe('');
    expect(sessionStorage.getItem('apihub-admin-token')).toBeNull();
  });

  it('sends the All API Hub backup with dry-run state and cancellation signal', async () => {
    const response = { data: { source:'all-api-hub', sourceVersion:'2.0', dryRun:true, summary:{ total:0,ready:0,created:0,skipped:0,duplicates:0,unsupported:0,invalid:0 }, items:[] } };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(response), { status:200, headers:{'Content-Type':'application/json'} }));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();
    const backup = { version:'2.0', timestamp:1710000000000, accounts:{ accounts:[] } };

    await expect(api.importSites(backup, true, controller.signal)).resolves.toMatchObject({ dryRun:true, sourceVersion:'2.0' });

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/site-imports', expect.objectContaining({ method:'POST', signal:controller.signal }));
    const request = fetchMock.mock.calls[0]![1] as RequestInit;
    expect(JSON.parse(String(request.body))).toEqual({ source:'all-api-hub', dryRun:true, backup });
  });
});
