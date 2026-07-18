import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';
import { describe, expect, it, vi } from 'vitest';
import { api } from '@/api';
import type { Site } from '@/types';
import SiteEditorView from '@/views/SiteEditorView.vue';

const createdSite: Site = {
  id: 'site-1',
  name: 'Example',
  baseUrl: 'https://example.com',
  adapter: 'new-api',
  userId: '42',
  enabled: true,
  checkinEnabled: true,
  announcementEnabled: true,
  checkinCron: '15 8 * * *',
  announcementCron: '*/30 * * * *',
  timezone: 'Asia/Shanghai',
  credentialConfigured: true,
  consecutiveFailures: 0,
  capabilities: { checkin: true, announcements: true, requiresUserId: true },
  createdAt: '2026-07-18T00:00:00Z',
  updatedAt: '2026-07-18T00:00:00Z',
};

describe('SiteEditorView', () => {
  it('submits a create request only once when the form fires twice', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/sites/new', component: SiteEditorView },
        { path: '/sites', component: defineComponent({ template: '<div>Sites</div>' }) },
      ],
    });
    await router.push('/sites/new');
    await router.isReady();

    vi.spyOn(api, 'adapters').mockResolvedValue([]);
    let resolveCreate!: (site: Site) => void;
    const create = vi.spyOn(api, 'createSite').mockImplementation(() => new Promise((resolve) => {
      resolveCreate = resolve;
    }));
    const wrapper = mount(SiteEditorView, { global: { plugins: [router] } });
    await flushPromises();

    const inputs = wrapper.findAll('input');
    await inputs[0]!.setValue('Example');
    await inputs[1]!.setValue('https://example.com');
    await inputs[3]!.setValue('station-token');

    const form = wrapper.get('form');
    const first = form.trigger('submit');
    const second = form.trigger('submit');
    await Promise.all([first, second]);

    expect(create).toHaveBeenCalledTimes(1);
    resolveCreate(createdSite);
    await flushPromises();
  });

  it('resets the form when a reused edit route changes to create', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/sites/new', component: SiteEditorView },
        { path: '/sites/:id/edit', component: SiteEditorView },
      ],
    });
    vi.spyOn(api, 'adapters').mockResolvedValue([]);
    vi.spyOn(api, 'site').mockResolvedValue(createdSite);
    await router.push('/sites/site-1/edit');
    await router.isReady();

    const wrapper = mount(RouterView, { global: { plugins: [router] } });
    await flushPromises();
    expect(wrapper.get('input').element.value).toBe('Example');

    await router.push('/sites/new');
    await flushPromises();

    expect(wrapper.get('input').element.value).toBe('');
    expect(wrapper.get('input[type="password"]').attributes('required')).toBeDefined();
    expect(wrapper.get('button[type="submit"]').text()).toBe('添加站点');
  });
});
