const ALARM = 'apihub-companion-poll';
const RUN_STATE = 'apihub-companion-active-run';
const ACTIVE = new Set();
const CANCELLED = new Set();
let pollPromise = null;
importScripts('url.js');
const { apiUrl, normalizeApiBase } = self.ApiHubUrl;

async function settings() {
  return chrome.storage.local.get({ apiBase: '', deviceToken: '', deviceName: '', foregroundOnManual: true });
}

async function loadRunState() {
  const state = await chrome.storage.session.get({ [RUN_STATE]: null });
  return state[RUN_STATE];
}

async function saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt) {
  await chrome.storage.session.set({ [RUN_STATE]: { task, tabId, actedUrl, manualSeen, deadlineAt } });
}

async function clearRunState(taskId) {
  const state = await loadRunState();
  if (!state || state.task?.id === taskId) await chrome.storage.session.remove(RUN_STATE);
}

async function request(path, options = {}) {
  const state = await settings();
  if (!state.apiBase || !state.deviceToken) throw new Error('浏览器伴侣尚未配对');
  const headers = new Headers(options.headers);
  headers.set('Authorization', `Bearer ${state.deviceToken}`);
  if (options.body) headers.set('Content-Type', 'application/json');
  const response = await fetch(apiUrl(state.apiBase, path), { ...options, headers });
  if (response.status === 204) return null;
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload?.error?.message || `APIHub 返回 HTTP ${response.status}`);
  return payload.data;
}

async function pair(apiBase, code, deviceName) {
	const normalizedBase = normalizeApiBase(apiBase);
	const response = await fetch(apiUrl(normalizedBase, '/api/v1/companion/pairings'), {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, deviceName }),
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload?.error?.message || '配对失败');
	await chrome.storage.local.set({ apiBase: normalizedBase, deviceToken: payload.data.deviceToken, deviceName: payload.data.device.name, lastError: '' });
	await chrome.storage.session.remove(RUN_STATE);
  await chrome.alarms.create(ALARM, { periodInMinutes: 1 });
  return payload.data.device;
}

async function waitForLoad(tabId, timeout = 30_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const current = await chrome.tabs.get(tabId);
    if (current.status === 'complete') return;
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error('页面加载超时');
}

async function inspect(tabId, act) {
  try { return await chrome.tabs.sendMessage(tabId, { type: 'apihub:inspect', act }); } catch { return { state: 'waiting', message: '等待页面脚本加载' }; }
}

async function waitForPageSignal(tabId) {
  try { await chrome.tabs.sendMessage(tabId, { type: 'apihub:wait' }); } catch { await waitForLoad(tabId).catch(() => undefined); }
}

async function focus(tabId) {
  const tab = await chrome.tabs.get(tabId);
  if (tab.windowId !== undefined) await chrome.windows.update(tab.windowId, { focused: true });
  await chrome.tabs.update(tabId, { active: true });
}

async function report(task, status, message, balance) {
  return request(`/api/v1/companion/tasks/${task.id}/results`, {
    method: 'POST', body: JSON.stringify({ leaseToken: task.leaseToken, status, message: String(message || '').slice(0, 500), ...(balance ? { balance: String(balance).slice(0, 128) } : {}) }),
  });
}

async function runTask(task, persisted = null) {
  if (ACTIVE.has(task.id)) return;
  ACTIVE.add(task.id);
  CANCELLED.delete(task.id);
  let tabId = persisted?.tabId;
  let lastHeartbeat = 0;
  let actedUrl = persisted?.actedUrl || '';
  let manualSeen = persisted?.manualSeen === true;
  const deadlineAt = persisted?.deadlineAt || Date.now() + 10 * 60_000;
  try {
    await saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt);
    if (tabId) {
      try { await chrome.tabs.get(tabId); } catch { tabId = undefined; }
    }
    if (!tabId) {
      const tab = await chrome.tabs.create({ url: task.targetUrl, active: false });
      tabId = tab.id;
      if (!tabId) throw new Error('无法创建签到标签页');
      await saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt);
    }
    await waitForLoad(tabId);
    while (Date.now() < deadlineAt && !CANCELLED.has(task.id)) {
      if (Date.now() - lastHeartbeat > 45_000) {
        await request(`/api/v1/companion/tasks/${task.id}/heartbeats`, { method: 'POST', headers: { 'X-Companion-Lease': task.leaseToken } });
        lastHeartbeat = Date.now();
      }
      const current = await chrome.tabs.get(tabId);
      const result = await inspect(tabId, current.url !== actedUrl);
      if (result.state === 'success' || result.state === 'already_checked') {
        await report(task, result.state, result.message, result.balance);
        await chrome.tabs.remove(tabId).catch(() => undefined);
        tabId = undefined;
        return;
      }
      if (result.state === 'manual_required') {
        manualSeen = true;
        await saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt);
        const state = await settings();
        if (state.foregroundOnManual) await focus(tabId);
        await chrome.action.setBadgeText({ text: '!' });
        await chrome.action.setBadgeBackgroundColor({ color: '#D97706' });
      }
      if (result.state === 'action') {
        actedUrl = current.url || task.targetUrl;
        await saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt);
      }
      await waitForPageSignal(tabId);
    }
    if (CANCELLED.has(task.id)) return;
    await report(task, 'manual_required', manualSeen ? '等待用户完成登录或人机验证超时' : '页面未出现可执行的签到入口');
  } catch (error) {
    await report(task, tabId ? 'manual_required' : 'failed', error instanceof Error ? error.message : '浏览器任务失败').catch(() => undefined);
  } finally {
    ACTIVE.delete(task.id);
    CANCELLED.delete(task.id);
    await clearRunState(task.id);
    await chrome.action.setBadgeText({ text: '' });
    if (tabId && manualSeen && (await settings()).foregroundOnManual) await focus(tabId).catch(() => undefined);
  }
}

async function pollOnce() {
  const persisted = await loadRunState();
  if (persisted?.task) {
    await runTask(persisted.task, persisted);
    return;
  }
  if (ACTIVE.size > 0) return;
  try {
    const task = await request('/api/v1/companion/tasks/claims', { method: 'POST' });
    await chrome.storage.local.set({ lastError: '', lastPollAt: new Date().toISOString() });
    if (task) await runTask(task);
  } catch (error) {
    await chrome.storage.local.set({ lastError: error instanceof Error ? error.message : '轮询失败', lastPollAt: new Date().toISOString() });
  }
}

function poll() {
  if (pollPromise) return pollPromise;
  pollPromise = pollOnce().finally(() => { pollPromise = null; });
  return pollPromise;
}

async function ensureAlarm() {
  const state = await settings();
  if (state.apiBase && state.deviceToken) await chrome.alarms.create(ALARM, { periodInMinutes: 1 });
  else await chrome.alarms.clear(ALARM);
}

async function disconnect() {
  const persisted = await loadRunState();
  if (persisted?.task) {
    CANCELLED.add(persisted.task.id);
    await report(persisted.task, 'failed', '设备已在本机断开连接').catch(() => undefined);
    if (persisted.tabId) await chrome.tabs.remove(persisted.tabId).catch(() => undefined);
    await clearRunState(persisted.task.id);
  }
  await chrome.storage.local.remove(['apiBase', 'deviceToken', 'deviceName']);
  await chrome.alarms.clear(ALARM);
}

chrome.runtime.onInstalled.addListener(() => { void ensureAlarm(); });
chrome.runtime.onStartup.addListener(() => { void ensureAlarm(); });
chrome.alarms.onAlarm.addListener((alarm) => { if (alarm.name === ALARM) void poll(); });
chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  const action = message?.type === 'apihub:pair' ? pair(message.apiBase, message.code, message.deviceName)
    : message?.type === 'apihub:poll' ? poll().then(() => true)
      : message?.type === 'apihub:disconnect' ? disconnect().then(() => true)
        : null;
  if (!action) return;
  action.then((data) => sendResponse({ ok: true, data }), (error) => sendResponse({ ok: false, error: error instanceof Error ? error.message : '操作失败' }));
  return true;
});
