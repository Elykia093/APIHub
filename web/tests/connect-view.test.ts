import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api';
import { clearSession, sessionToken } from '@/session';
import type { Summary } from '@/types';
import ConnectView from '@/views/ConnectView.vue';

describe('ConnectView', () => {
  beforeEach(() => clearSession());

  it('stores only the token that was validated and ignores duplicate submits', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/connect', component: ConnectView },
        { path: '/', component: defineComponent({ template: '<div>Overview</div>' }) },
      ],
    });
    await router.push('/connect');
    await router.isReady();

    let resolveValidation!: (summary: Summary) => void;
    const validate = vi.spyOn(api, 'validateToken').mockImplementation(() => new Promise<Summary>((resolve) => {
      resolveValidation = resolve;
    }));
    const wrapper = mount(ConnectView, { global: { plugins: [router] } });
    const input = wrapper.get('input');
    await input.setValue('validated-admin-token');

    const form = wrapper.get('form');
    await Promise.all([form.trigger('submit'), form.trigger('submit')]);
    await input.setValue('unvalidated-admin-token');
    resolveValidation({ sites: { total: 0, enabled: 0 }, today: {}, unreadAnnouncements: 0 });
    await flushPromises();

    expect(validate).toHaveBeenCalledTimes(1);
    expect(validate).toHaveBeenCalledWith('validated-admin-token');
    expect(sessionToken()).toBe('validated-admin-token');
  });
});
