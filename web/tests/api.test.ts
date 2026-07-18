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
});
