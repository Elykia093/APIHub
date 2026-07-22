import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api';
import type { Site } from '@/types';
import CompanionView from '@/views/CompanionView.vue';

const site: Site = { id:'site-1',name:'站点',baseUrl:'https://example.com',adapter:'new-api',userId:'1',enabled:true,checkinEnabled:true,announcementEnabled:true,checkinCron:'0 8 * * *',announcementCron:'0 * * * *',timezone:'Asia/Shanghai',credentialConfigured:true,consecutiveFailures:0,capabilities:{checkin:true,announcements:true,requiresUserId:true},createdAt:'2026-01-01T00:00:00Z',updatedAt:'2026-01-01T00:00:00Z' };

describe('CompanionView', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, 'sites').mockResolvedValue([site, { ...site, id: 'site-2', name: '第二站点', baseUrl: 'https://second.example' }]);
    vi.spyOn(api, 'companionDevices').mockResolvedValue([]);
    vi.spyOn(api, 'browserTasks').mockResolvedValue([]);
  });

  it('creates and renders a one-time pairing code', async () => {
    vi.spyOn(api, 'createPairingCode').mockResolvedValue({ code: 'ABCDEF0123456789ABCDEF01', expiresAt: '2026-01-01T00:05:00Z' });
    const wrapper = mount(CompanionView);
    await flushPromises();
    const button = wrapper.findAll('button').find((candidate) => candidate.text() === '生成配对码');
    expect(button).toBeDefined();
    await button!.trigger('click');
    await flushPromises();
    expect(wrapper.text()).toContain('ABCDEF0123456789ABCDEF01');
  });

  it('updates the default target URL when the selected site changes', async () => {
    const wrapper = mount(CompanionView);
    await flushPromises();
    expect((wrapper.get('input[type="url"]').element as HTMLInputElement).value).toBe('https://example.com');
    await wrapper.get('select').setValue('site-2');
    await flushPromises();
    expect((wrapper.get('input[type="url"]').element as HTMLInputElement).value).toBe('https://second.example');
  });
});
