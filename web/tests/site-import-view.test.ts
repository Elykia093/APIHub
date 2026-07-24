import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import { api } from '@/api';
import type { SiteImportResult } from '@/types';
import SiteImportView from '@/views/SiteImportView.vue';

const preview: SiteImportResult = {
  source: 'all-api-hub',
  sourceVersion: '2.0',
  dryRun: true,
  summary: { total:3, ready:1, created:0, skipped:2, duplicates:1, unsupported:0, invalid:1 },
  items: [
    { index:0, name:'示例站点', baseUrl:'https://example.com', siteType:'new-api', adapter:'new-api', status:'ready' },
    { index:1, name:'重复站点', baseUrl:'https://example.com', siteType:'new-api', adapter:'new-api', status:'skipped', reasonCode:'DUPLICATE_IN_BACKUP' },
    { index:2, name:'旧版响应', baseUrl:'', siteType:'new-api', adapter:'new-api', status:'skipped', reason:'服务端返回的兼容原因' },
  ],
};

const applied: SiteImportResult = {
  ...preview,
  dryRun: false,
  summary: { ...preview.summary, created:1 },
  items: [{ ...preview.items[0]!, status:'created' }, preview.items[1]!],
};

function attachFile(wrapper: ReturnType<typeof mount>, content: string) {
  const input = wrapper.get('input[type="file"]');
  const file = { name:'backup.json', size:new TextEncoder().encode(content).byteLength, text:vi.fn().mockResolvedValue(content) } as unknown as File;
  Object.defineProperty(input.element, 'files', { configurable:true, value:[file] });
  return input.trigger('change');
}

describe('SiteImportView', () => {
  it('previews and applies a backup without rendering credentials', async () => {
    const importSites = vi.spyOn(api, 'importSites').mockResolvedValueOnce(preview).mockResolvedValueOnce(applied);
    const wrapper = mount(SiteImportView, { global:{ stubs:{ RouterLink:{ template:'<a><slot /></a>' } } } });
    const secret = 'backup-token-must-not-render';
    const backup = { version:'2.0', timestamp:1710000000000, accounts:{ accounts:[{ site_name:'示例站点', site_url:'https://example.com', site_type:'new-api', account_info:{ id:'1', access_token:secret } }] } };

    await attachFile(wrapper, JSON.stringify(backup));
    await flushPromises();
    await wrapper.get('button').trigger('click');
    await flushPromises();

    expect(importSites).toHaveBeenNthCalledWith(1, backup, true, expect.any(AbortSignal));
    expect(wrapper.text()).toContain('导入预览');
    expect(wrapper.text()).toContain('备份中存在相同站点地址');
    expect(wrapper.text()).toContain('服务端返回的兼容原因');
    expect(wrapper.text()).not.toContain(secret);

    await wrapper.findAll('button').find((button) => button.text().includes('导入 1 个站点'))!.trigger('click');
    await flushPromises();

    expect(importSites).toHaveBeenNthCalledWith(2, backup, false, expect.any(AbortSignal));
    expect(wrapper.text()).toContain('导入完成');
    expect(wrapper.text()).toContain('已导入');
    expect(wrapper.text()).not.toContain(secret);
  });

  it('rejects malformed JSON before calling the API', async () => {
    const importSites = vi.spyOn(api, 'importSites');
    const wrapper = mount(SiteImportView, { global:{ stubs:{ RouterLink:{ template:'<a><slot /></a>' } } } });

    await attachFile(wrapper, '{not-json');
    await flushPromises();

    expect(wrapper.get('[role="alert"]').text()).toContain('无法读取 JSON 备份');
    expect(wrapper.get('button').attributes('disabled')).toBeDefined();
    expect(importSites).not.toHaveBeenCalled();
  });
});
