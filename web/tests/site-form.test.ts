import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import SiteForm from '@/components/SiteForm.vue';
import type { AdapterDescriptor, Site, SiteWrite } from '@/types';

const adapters: AdapterDescriptor[] = [
  { name:'new-api',displayName:'New API',capabilities:{checkin:true,announcements:true,requiresUserId:true} },
  { name:'sub2api',displayName:'Sub2API',capabilities:{checkin:false,announcements:true,requiresUserId:false} },
];
const site: Site = { id:'site-1',name:'Example',baseUrl:'https://example.com',adapter:'new-api',userId:'42',enabled:false,checkinEnabled:true,announcementEnabled:true,checkinCron:'15 8 * * *',announcementCron:'*/30 * * * *',timezone:'Asia/Shanghai',credentialConfigured:true,consecutiveFailures:0,capabilities:adapters[0]!.capabilities,createdAt:'2026-07-17T00:00:00Z',updatedAt:'2026-07-17T00:00:00Z' };

describe('SiteForm', () => {
  it('preserves false and omits an empty credential while editing', async () => {
    const wrapper = mount(SiteForm,{props:{site,adapters,submitting:false}});
    await wrapper.get('form').trigger('submit');
    const payload = wrapper.emitted('submit')?.[0]?.[0] as SiteWrite | undefined;
    expect(payload?.enabled).toBe(false);
    expect(payload).not.toHaveProperty('accessToken');
  });

  it('lets automatic detection choose capability defaults', async () => {
    const wrapper = mount(SiteForm,{props:{site:undefined,adapters,submitting:false}});
    await wrapper.get('form').trigger('submit');
    const payload = wrapper.emitted('submit')?.[0]?.[0] as SiteWrite | undefined;
    expect(payload?.adapter).toBe('auto');
    expect(payload).not.toHaveProperty('checkinEnabled');
    expect(payload).not.toHaveProperty('announcementEnabled');
  });

  it('turns off a capability that the selected adapter cannot provide', async () => {
    const wrapper = mount(SiteForm,{props:{site,adapters,submitting:false}});
    await wrapper.get('select').setValue('sub2api');
    await wrapper.get('form').trigger('submit');
    const payload = wrapper.emitted('submit')?.[0]?.[0] as SiteWrite | undefined;
    expect(payload?.checkinEnabled).toBe(false);
    expect(payload?.announcementEnabled).toBe(true);
  });
});
