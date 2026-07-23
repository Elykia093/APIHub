const ALARM = 'apihub-companion-poll';
const RUN_STATE = 'apihub-companion-active-run';
const ACTIVE = new Set();
const ACTIVE_RUNS = new Map();
const CANCELLED = new Set();
let pollPromise = null;
let pollStartPromise = null;
let controlActive = false;
const REQUEST_TIMEOUT = 15_000;
const REPORT_ATTEMPTS = 3;
const REPORT_DELAYS = [500, 1_500];
importScripts('url.js');
const { apiUrl, normalizeApiBase } = self.ApiHubUrl;

class APIRequestError extends Error {
  constructor(message, status = 0) {
    super(message);
    this.name = 'APIRequestError';
    this.status = status;
  }
}

async function settings() {
  return chrome.storage.local.get({ apiBase: '', deviceToken: '', deviceName: '', foregroundOnManual: true });
}

async function loadRunState() {
  const state = await chrome.storage.session.get({ [RUN_STATE]: null });
  return state[RUN_STATE];
}

async function saveRunState(task, tabId, actedUrl, manualSeen, deadlineAt, pendingResult = null) {
  await chrome.storage.session.set({ [RUN_STATE]: { task, tabId, actedUrl, manualSeen, deadlineAt, pendingResult } });
}

async function clearRunState(taskId) {
  const state = await loadRunState();
  if (!state || state.task?.id === taskId) await chrome.storage.session.remove(RUN_STATE);
}

async function request(path, options = {}, credentials = null) {
  const state = credentials || await settings();
  if (!state.apiBase || !state.deviceToken) throw new Error('浏览器伴侣尚未配对');
  const headers = new Headers(options.headers);
  headers.set('Authorization', `Bearer ${state.deviceToken}`);
  if (options.body) headers.set('Content-Type', 'application/json');
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT);
  try {
    const response = await fetch(apiUrl(state.apiBase, path), { ...options, headers, signal: controller.signal });
    if (response.status === 204) return null;
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new APIRequestError(payload?.error?.message || `APIHub 返回 HTTP ${response.status}`, response.status);
    return payload.data;
  } catch (error) {
    if (error?.name === 'AbortError') throw new APIRequestError('APIHub 请求超时');
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}

async function pairImpl(apiBase, code, deviceName) {
  const normalizedBase = normalizeApiBase(apiBase);
  if (!await stopActiveRun('设备已重新配对')) throw new Error('旧任务结果尚未确认，暂不能替换配对');
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT);
  let response;
  try {
    response = await fetch(apiUrl(normalizedBase, '/api/v1/companion/pairings'), {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, deviceName }), signal: controller.signal,
    });
  } catch (error) {
    if (error?.name === 'AbortError') throw new APIRequestError('配对请求超时');
    throw error;
  } finally {
    clearTimeout(timeout);
  }
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new APIRequestError(payload?.error?.message || '配对失败', response.status);
  await chrome.storage.local.set({ apiBase: normalizedBase, deviceToken: payload.data.deviceToken, deviceName: payload.data.device.name, lastError: '' });
  await chrome.alarms.create(ALARM, { periodInMinutes: 1 });
  return payload.data.device;
}

async function pair(apiBase, code, deviceName) {
  if (controlActive) throw new Error('另一个设备控制操作正在进行');
  controlActive = true;
  try {
    return await pairImpl(apiBase, code, deviceName);
  } finally {
    controlActive = false;
  }
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

async function heartbeat(task, credentials) {
  return request(`/api/v1/companion/tasks/${task.id}/heartbeats`, {
    method: 'POST', headers: { 'X-Companion-Lease': task.leaseToken },
  }, credentials);
}

async function report(task, result, credentials) {
  return request(`/api/v1/companion/tasks/${task.id}/results`, {
    method: 'POST', body: JSON.stringify({
      leaseToken: task.leaseToken,
      status: result.status,
      message: String(result.message || '').slice(0, 500),
      ...(result.balance ? { balance: String(result.balance).slice(0, 128) } : {}),
    }),
  }, credentials);
}

function retryableReportError(error) {
  return !(error instanceof APIRequestError) || error.status === 0 || error.status === 408 || error.status === 429 || error.status >= 500;
}

async function deliverResult(state, result, credentials) {
  await saveRunState(state.task, state.tabId, state.actedUrl, state.manualSeen, state.deadlineAt, result);
  let lastError;
  for (let attempt = 0; attempt < REPORT_ATTEMPTS; attempt += 1) {
    try {
      await report(state.task, result, credentials);
      return { delivered: true, retryable: false };
    } catch (error) {
      lastError = error;
      if (!retryableReportError(error)) return { delivered: false, retryable: false, error };
      if (attempt + 1 < REPORT_ATTEMPTS) {
        await heartbeat(state.task, credentials).catch(() => undefined);
        await new Promise((resolve) => setTimeout(resolve, REPORT_DELAYS[attempt]));
      }
    }
  }
  return { delivered: false, retryable: true, error: lastError };
}

function setOutcome(run, outcome) {
  run.finished = outcome.delivered || !outcome.retryable;
  run.retryPending = !outcome.delivered && outcome.retryable;
  if (outcome.error) void chrome.storage.local.set({ lastError: outcome.error instanceof Error ? outcome.error.message : '任务结果回报失败' });
}

async function closeTerminalTab(run, result, outcome) {
  if (outcome.retryable || result.status === 'manual_required' || !run.tabId) return;
  await chrome.tabs.remove(run.tabId).catch(() => undefined);
  run.tabId = undefined;
}

async function executeTask(run, persisted) {
  const { task } = run;
  run.credentials = await settings();
  let lastHeartbeat = 0;
  run.actedUrl = persisted?.actedUrl || '';
  run.manualSeen = persisted?.manualSeen === true;
  run.deadlineAt = persisted?.deadlineAt || Date.now() + 10 * 60_000;
  try {
    await saveRunState(task, run.tabId, run.actedUrl, run.manualSeen, run.deadlineAt, persisted?.pendingResult || null);
    if (run.cancelled) return;
    if (persisted?.pendingResult) {
      const outcome = await deliverResult({ ...persisted, task }, persisted.pendingResult, run.credentials);
      setOutcome(run, outcome);
      await closeTerminalTab(run, persisted.pendingResult, outcome);
      return;
    }
    if (run.tabId) {
      try { await chrome.tabs.get(run.tabId); } catch { run.tabId = undefined; }
    }
    if (!run.tabId) {
      const tab = await chrome.tabs.create({ url: task.targetUrl, active: false });
      run.tabId = tab.id;
      if (!run.tabId) throw new Error('无法创建签到标签页');
      await saveRunState(task, run.tabId, run.actedUrl, run.manualSeen, run.deadlineAt);
    }
    if (run.cancelled) return;
    await waitForLoad(run.tabId);
    while (Date.now() < run.deadlineAt && !run.cancelled && !CANCELLED.has(task.id)) {
      if (Date.now() - lastHeartbeat > 45_000) {
        await heartbeat(task, run.credentials);
        lastHeartbeat = Date.now();
      }
      const current = await chrome.tabs.get(run.tabId);
      const result = await inspect(run.tabId, current.url !== run.actedUrl);
      if (result.state === 'success' || result.state === 'already_checked') {
        const terminalResult = { status: result.state, message: result.message, balance: result.balance };
        const outcome = await deliverResult(run, terminalResult, run.credentials);
        setOutcome(run, outcome);
        await closeTerminalTab(run, terminalResult, outcome);
        return;
      }
      if (result.state === 'manual_required') {
        run.manualSeen = true;
        await saveRunState(task, run.tabId, run.actedUrl, run.manualSeen, run.deadlineAt);
        const state = await settings();
        if (state.foregroundOnManual) await focus(run.tabId);
        await chrome.action.setBadgeText({ text: '!' });
        await chrome.action.setBadgeBackgroundColor({ color: '#D97706' });
      }
      if (result.state === 'action') {
        run.actedUrl = current.url || task.targetUrl;
        await saveRunState(task, run.tabId, run.actedUrl, run.manualSeen, run.deadlineAt);
      }
      await waitForPageSignal(run.tabId);
    }
    if (run.cancelled || CANCELLED.has(task.id)) return;
    setOutcome(run, await deliverResult(run, {
      status: 'manual_required',
      message: run.manualSeen ? '等待用户完成登录或人机验证超时' : '页面未出现可执行的签到入口',
    }, run.credentials));
  } catch (error) {
    if (!run.cancelled) {
      const terminalResult = {
        status: run.manualSeen ? 'manual_required' : 'failed',
        message: error instanceof Error ? error.message : '浏览器任务失败',
      };
      const outcome = await deliverResult(run, terminalResult, run.credentials);
      setOutcome(run, outcome);
      await closeTerminalTab(run, terminalResult, outcome);
    }
  }
}

function runTask(task, persisted = null) {
  const existing = ACTIVE_RUNS.get(task.id);
  if (existing) return existing.promise;
  const run = {
    task,
    tabId: persisted?.tabId,
    actedUrl: persisted?.actedUrl || '',
    manualSeen: persisted?.manualSeen === true,
    deadlineAt: persisted?.deadlineAt || Date.now() + 10 * 60_000,
    credentials: null,
    cancelled: false,
    keepState: false,
    finished: false,
    retryPending: false,
    promise: null,
  };
  ACTIVE.add(task.id);
  CANCELLED.delete(task.id);
  run.promise = executeTask(run, persisted).finally(async () => {
    if (run.finished && !run.keepState) await clearRunState(task.id);
    ACTIVE.delete(task.id);
    ACTIVE_RUNS.delete(task.id);
    CANCELLED.delete(task.id);
    await chrome.action.setBadgeText({ text: '' });
    if (run.tabId && run.manualSeen && !run.cancelled && (await settings()).foregroundOnManual) await focus(run.tabId).catch(() => undefined);
  });
  ACTIVE_RUNS.set(task.id, run);
  return run.promise;
}

async function pollOnce() {
  if (controlActive) return;
  let resolvePollStart;
  const currentPollStart = new Promise((resolve) => { resolvePollStart = resolve; });
  pollStartPromise = currentPollStart;
  const markPollStarted = () => {
    resolvePollStart();
    if (pollStartPromise === currentPollStart) pollStartPromise = null;
  };
  try {
    const persisted = await loadRunState();
    if (persisted?.task) {
      const run = runTask(persisted.task, persisted);
      markPollStarted();
      await run;
      return;
    }
    if (ACTIVE.size > 0) {
      markPollStarted();
      return;
    }
    const task = await request('/api/v1/companion/tasks/claims', { method: 'POST' });
    await chrome.storage.local.set({ lastError: '', lastPollAt: new Date().toISOString() });
    if (task) {
      const run = runTask(task);
      markPollStarted();
      await run;
    } else {
      markPollStarted();
    }
  } catch (error) {
    markPollStarted();
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

async function stopActiveRun(reason) {
  if (pollStartPromise) await pollStartPromise.catch(() => undefined);
  const active = ACTIVE_RUNS.values().next().value;
  const persisted = await loadRunState();
  const task = active?.task || persisted?.task;
  if (!task) return true;
  const credentials = active?.credentials || await settings();
  const tabId = active?.tabId || persisted?.tabId;
  if (active) {
    active.cancelled = true;
    active.keepState = true;
    CANCELLED.add(task.id);
    await active.promise;
  }
  const state = await loadRunState() || persisted || {
    task,
    tabId,
    actedUrl: active?.actedUrl || '',
    manualSeen: active?.manualSeen === true,
    deadlineAt: active?.deadlineAt || Date.now(),
  };
  const result = state.pendingResult || { status: 'failed', message: reason };
  const outcome = await deliverResult({ ...state, task, tabId: state.tabId || tabId }, result, credentials);
  if (outcome.delivered || !outcome.retryable) await clearRunState(task.id);
  if (outcome.error) await chrome.storage.local.set({ lastError: outcome.error instanceof Error ? outcome.error.message : '任务结果回报失败' });
  if (tabId) await chrome.tabs.remove(tabId).catch(() => undefined);
  return outcome.delivered || !outcome.retryable;
}

async function disconnectImpl() {
  const stopped = await stopActiveRun('设备已在本机断开连接');
  if (!stopped) throw new Error('任务结果尚未确认，网络恢复后再断开');
  await chrome.storage.local.remove(['apiBase', 'deviceToken', 'deviceName']);
  await chrome.alarms.clear(ALARM);
  return true;
}

async function disconnect() {
  if (controlActive) throw new Error('另一个设备控制操作正在进行');
  controlActive = true;
  try {
    return await disconnectImpl();
  } finally {
    controlActive = false;
  }
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
