<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { api, APIError } from '@/api';
import AppIcon from '@/components/AppIcon.vue';
import type { SiteImportItem, SiteImportResult } from '@/types';

const maxImportBytes = 4 * 1024 * 1024;
const backup = ref<Record<string, unknown> | null>(null);
const fileName = ref('');
const fileSize = ref(0);
const localError = ref('');
const requestError = ref('');
const result = ref<SiteImportResult>();
const busy = ref(false);
const activeDryRun = ref<boolean>();
const revision = ref(0);
let requestController: AbortController | undefined;

const readyCount = computed(() => result.value?.summary.ready ?? 0);

const reasonLabels: Record<string, string> = {
  INVALID_ACCOUNT: '账号字段不完整或格式不正确',
  UNSUPPORTED_SITE_TYPE: '站点类型暂不支持',
  UNSUPPORTED_AUTH_TYPE: '仅支持访问令牌账号',
  DUPLICATE_IN_BACKUP: '备份中存在相同站点地址',
  ALREADY_EXISTS: '相同站点地址已存在',
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MiB`;
}

function cancelRequest() {
  requestController?.abort();
  requestController = undefined;
}

function resetSelection() {
  backup.value = null;
  fileName.value = '';
  fileSize.value = 0;
  result.value = undefined;
  localError.value = '';
  requestError.value = '';
  busy.value = false;
  activeDryRun.value = undefined;
}

async function selectFile(event: Event) {
  cancelRequest();
  const currentRevision = ++revision.value;
  resetSelection();
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;

  fileName.value = file.name;
  fileSize.value = file.size;
  if (file.size > maxImportBytes) {
    localError.value = '文件超过 4 MiB 上限，请选择较小的 JSON 备份。';
    return;
  }

  try {
    const parsed: unknown = JSON.parse(await file.text());
    if (!isRecord(parsed)) throw new Error('backup must be an object');
    if (!Object.prototype.hasOwnProperty.call(parsed, 'timestamp')) throw new Error('timestamp is required');
    if (currentRevision !== revision.value) return;
    backup.value = parsed;
  } catch {
    if (currentRevision === revision.value) localError.value = '无法读取 JSON 备份，请检查文件内容后重试。';
  }
}

function statusLabel(status: SiteImportItem['status']): string {
  return status === 'ready' ? '可导入' : status === 'created' ? '已导入' : '已跳过';
}

function reasonLabel(item: SiteImportItem): string {
  if (!item.reasonCode) return item.reason ?? '';
  return reasonLabels[item.reasonCode] ?? item.reason ?? '服务器拒绝导入';
}

function itemStatusClass(status: SiteImportItem['status']): string {
  return status === 'created' ? 'is-created' : status === 'ready' ? 'is-ready' : 'is-skipped';
}

async function submit(dryRun: boolean) {
  if (!backup.value || busy.value) return;
  const requestRevision = revision.value;
  const requestBody = JSON.stringify({ source: 'all-api-hub', dryRun, backup: backup.value });
  if (new TextEncoder().encode(requestBody).byteLength > maxImportBytes) {
    requestError.value = '发送内容超过 4 MiB 上限，请选择较小的 JSON 备份。';
    return;
  }
  cancelRequest();
  requestController = new AbortController();
  busy.value = true;
  activeDryRun.value = dryRun;
  localError.value = '';
  requestError.value = '';
  try {
    const nextResult = await api.importSites(backup.value, dryRun, requestController.signal);
    if (requestRevision === revision.value) result.value = nextResult;
  } catch (caught) {
    if (requestRevision !== revision.value) return;
    if (caught instanceof APIError) requestError.value = caught.message;
    else if (!(caught instanceof DOMException && caught.name === 'AbortError')) requestError.value = '导入失败，请稍后重试。';
  } finally {
    if (requestRevision === revision.value) {
      busy.value = false;
      activeDryRun.value = undefined;
      requestController = undefined;
    }
  }
}

onBeforeUnmount(cancelRequest);
</script>

<template>
  <div class="page narrow-page site-import-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Import
        </p>
        <h1>导入站点备份</h1>
        <p>
          读取 All API Hub JSON 备份，先预览，再一次性写入站点。
        </p>
      </div>
      <RouterLink
        class="button ghost"
        to="/sites"
      >
        返回站点
      </RouterLink>
    </header>

    <section class="form-card import-upload-card">
      <div class="panel-heading">
        <div>
          <p class="eyebrow">
            Backup file
          </p>
          <h2>选择备份文件</h2>
          <p>
            仅解析站点账号字段，令牌不会显示在预览或页面日志中。
          </p>
        </div>
      </div>
      <label
        class="import-dropzone"
        :class="{ 'has-file': fileName, 'has-error': localError }"
        for="site-import-file"
      >
        <input
          id="site-import-file"
          accept="application/json,.json"
          type="file"
          @change="selectFile"
        >
        <span class="import-dropzone-icon"><AppIcon
          name="upload"
          :size="24"
        /></span>
        <strong>{{ fileName || '选择 JSON 备份文件' }}</strong>
        <small v-if="fileName">{{ formatBytes(fileSize) }} · 选择其他文件可重新预览</small>
        <small v-else>支持 4 MiB 以内的 All API Hub 备份</small>
      </label>
      <p
        v-if="localError"
        class="form-error banner"
        role="alert"
      >
        {{ localError }}
      </p>
      <p
        v-if="requestError"
        class="form-error banner"
        role="alert"
      >
        {{ requestError }}
      </p>
      <div class="form-actions import-actions">
        <button
          class="button primary"
          type="button"
          :disabled="!backup || busy"
          @click="submit(true)"
        >
          <AppIcon
            name="upload"
            :size="16"
          />
          {{ busy && activeDryRun ? '正在生成预览…' : '预览导入' }}
        </button>
      </div>
    </section>

    <section
      v-if="result"
      class="panel import-result"
      aria-live="polite"
    >
      <div class="panel-heading">
        <div>
          <p class="eyebrow">
            {{ result.dryRun ? 'Preview' : 'Applied' }}
          </p>
          <h2>{{ result.dryRun ? '导入预览' : '导入完成' }}</h2>
          <p>
            备份版本 {{ result.sourceVersion }} · 共 {{ result.summary.total }} 个账号
          </p>
        </div>
        <span
          class="import-mode-tag"
          :class="result.dryRun ? 'is-preview' : 'is-applied'"
        >{{ result.dryRun ? '预览模式' : '已应用' }}</span>
      </div>

      <div class="import-summary-grid">
        <div><span>可导入</span><strong>{{ result.summary.ready }}</strong></div>
        <div><span>{{ result.dryRun ? '预计新增' : '已新增' }}</span><strong>{{ result.dryRun ? result.summary.ready : result.summary.created }}</strong></div>
        <div><span>重复站点</span><strong>{{ result.summary.duplicates }}</strong></div>
        <div><span>已跳过</span><strong>{{ result.summary.skipped }}</strong></div>
      </div>

      <div class="import-table-wrap">
        <table>
          <thead>
            <tr><th>账号</th><th>站点地址</th><th>类型映射</th><th>结果</th></tr>
          </thead>
          <tbody>
            <tr
              v-for="item in result.items"
              :key="item.index"
            >
              <td><strong>{{ item.name || '未命名账号' }}</strong><small v-if="item.reasonCode || item.reason">{{ reasonLabel(item) }}</small></td>
              <td class="import-url">
                {{ item.baseUrl || '未提供' }}
              </td>
              <td>{{ item.adapter || item.siteType || '未知' }}</td>
              <td>
                <span
                  class="import-item-status"
                  :class="itemStatusClass(item.status)"
                >{{ statusLabel(item.status) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p
        v-if="result.dryRun && readyCount === 0"
        class="import-empty-note"
      >
        没有可导入的账号。请修正备份内容，或返回站点列表手动添加。
      </p>
      <div class="form-actions import-actions">
        <button
          v-if="result.dryRun && readyCount > 0"
          class="button primary"
          type="button"
          :disabled="busy"
          @click="submit(false)"
        >
          <AppIcon
            name="upload"
            :size="16"
          />
          {{ busy && activeDryRun === false ? '正在导入…' : `导入 ${readyCount} 个站点` }}
        </button>
        <button
          v-if="result.dryRun"
          class="button ghost"
          type="button"
          :disabled="busy"
          @click="submit(true)"
        >
          重新预览
        </button>
        <RouterLink
          v-else
          class="button primary"
          to="/sites"
        >
          查看站点列表
        </RouterLink>
      </div>
    </section>
  </div>
</template>
