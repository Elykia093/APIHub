<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { api, APIError } from '@/api';
import type { BrowserTask, CompanionDevice, Site } from '@/types';

const sites = ref<Site[]>([]);
const devices = ref<CompanionDevice[]>([]);
const tasks = ref<BrowserTask[]>([]);
const loading = ref(true);
const actionError = ref('');
const pairingCode = ref<{ code: string; expiresAt: string } | null>(null);
const selectedSiteId = ref('');
const targetUrl = ref('');
const pending = ref(new Set<string>());

watch(selectedSiteId, (siteId, previousSiteId) => {
  if (siteId === previousSiteId) return;
  targetUrl.value = sites.value.find((site) => site.id === siteId)?.baseUrl ?? '';
});

async function load() {
  loading.value = true;
  actionError.value = '';
  try {
    const [nextSites, nextDevices, nextTasks] = await Promise.all([api.sites(), api.companionDevices(), api.browserTasks()]);
    sites.value = nextSites;
    devices.value = nextDevices;
    tasks.value = nextTasks;
    if (!selectedSiteId.value) selectedSiteId.value = nextSites[0]?.id ?? '';
    if (!targetUrl.value && nextSites[0]) targetUrl.value = nextSites[0].baseUrl;
  } catch (error) {
    actionError.value = error instanceof APIError ? error.message : '无法加载浏览器伴侣状态';
  } finally {
    loading.value = false;
  }
}

function markPending(key: string, value: boolean) {
  const next = new Set(pending.value);
  if (value) next.add(key); else next.delete(key);
  pending.value = next;
}

async function createCode() {
  markPending('pair', true); actionError.value = '';
  try { pairingCode.value = await api.createPairingCode(); } catch (error) { actionError.value = error instanceof APIError ? error.message : '生成配对码失败'; } finally { markPending('pair', false); }
}

async function revoke(device: CompanionDevice) {
  markPending(device.id, true); actionError.value = '';
  try { await api.revokeCompanionDevice(device.id); await load(); } catch (error) { actionError.value = error instanceof APIError ? error.message : '撤销设备失败'; } finally { markPending(device.id, false); }
}

async function createTask() {
  if (!selectedSiteId.value || !targetUrl.value.trim()) return;
  markPending('task', true); actionError.value = '';
  try { await api.createBrowserTask(selectedSiteId.value, targetUrl.value.trim()); await load(); } catch (error) { actionError.value = error instanceof APIError ? error.message : '创建浏览器任务失败'; } finally { markPending('task', false); }
}

async function copyCode() {
  if (pairingCode.value) await navigator.clipboard.writeText(pairingCode.value.code);
}

onMounted(load);
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <p class="eyebrow">
          Browser companion
        </p><h1>浏览器伴侣</h1><p>任务在你的 Chrome 会话内执行，APIHub 不接收 Cookie 或本地存储内容。</p>
      </div>
      <button
        class="button secondary"
        type="button"
        :disabled="loading"
        @click="load"
      >
        刷新
      </button>
    </header>
    <p
      v-if="actionError"
      class="form-error banner"
      role="alert"
    >
      {{ actionError }}
    </p>
    <section class="content-grid">
      <article class="panel">
        <div class="panel-heading">
          <div>
            <p class="eyebrow">
              Pairing
            </p><h2>配对设备</h2>
          </div><button
            class="button primary compact"
            type="button"
            :disabled="pending.has('pair')"
            @click="createCode"
          >
            生成配对码
          </button>
        </div>
        <p class="muted">
          配对码五分钟内有效且只能使用一次。扩展配对后，令牌只保存在该 Chrome 配置中。
        </p>
        <div
          v-if="pairingCode"
          class="pairing-code"
        >
          <code>{{ pairingCode.code }}</code><button
            class="button ghost compact"
            type="button"
            @click="copyCode"
          >
            复制
          </button><small>有效至 {{ new Date(pairingCode.expiresAt).toLocaleTimeString() }}</small>
        </div>
        <div class="stack-list">
          <div
            v-for="device in devices"
            :key="device.id"
            class="list-row"
          >
            <div><strong>{{ device.name }}</strong><small>{{ device.revokedAt ? '已撤销' : device.lastSeenAt ? `最近在线 ${new Date(device.lastSeenAt).toLocaleString()}` : '尚未连接' }}</small></div><button
              v-if="!device.revokedAt"
              class="button ghost compact"
              type="button"
              :disabled="pending.has(device.id)"
              @click="revoke(device)"
            >
              撤销
            </button>
          </div><p
            v-if="devices.length===0"
            class="empty-inline"
          >
            暂无已配对设备
          </p>
        </div>
      </article>
      <article class="panel">
        <div class="panel-heading">
          <div>
            <p class="eyebrow">
              Task
            </p><h2>创建浏览器签到</h2>
          </div>
        </div>
        <form
          class="stack-form"
          @submit.prevent="createTask"
        >
          <label>站点<select v-model="selectedSiteId"><option
            v-for="site in sites"
            :key="site.id"
            :value="site.id"
          >{{ site.name }}</option></select></label><label>签到页 URL<input
            v-model="targetUrl"
            type="url"
            required
            maxlength="2048"
            placeholder="https://example.com/console/personal"
          ></label><button
            class="button primary"
            type="submit"
            :disabled="pending.has('task') || !selectedSiteId"
          >
            下发浏览器任务
          </button>
        </form>
        <p class="muted">
          URL 必须与站点地址同源。扩展会复用当前页面登录态；遇到 OAuth 或人机验证时会把页面前置，由你完成。
        </p>
      </article>
    </section>
    <section class="panel table-card">
      <div class="panel-heading">
        <div>
          <p class="eyebrow">
            Runs
          </p><h2>浏览器任务</h2>
        </div>
      </div><table>
        <thead><tr><th>站点</th><th>状态</th><th>尝试</th><th>余额</th><th>说明</th><th>创建时间</th></tr></thead><tbody>
          <tr
            v-for="task in tasks"
            :key="task.id"
          >
            <td><strong>{{ task.siteName ?? task.siteId }}</strong><small>{{ task.targetUrl }}</small></td><td>{{ task.status }}</td><td>{{ task.attemptCount }}</td><td>{{ task.balance ?? '—' }}</td><td>{{ task.message || '—' }}</td><td>{{ new Date(task.createdAt).toLocaleString() }}</td>
          </tr>
        </tbody>
      </table><p
        v-if="!loading && tasks.length===0"
        class="empty-inline"
      >
        暂无浏览器任务
      </p>
    </section>
  </div>
</template>
