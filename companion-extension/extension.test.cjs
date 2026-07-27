const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');
const { normalizeApiBase } = require('./url.js');

const response = (data, status = 200) => ({ status, ok: status >= 200 && status < 300, json: async () => data });

function loadBackground({ fetchImpl, inspectImpl = async () => ({ state: 'success', message: 'ok' }), gateFirstSessionSet = false, localState, sessionState, tabsState }) {
  const local = localState || new Map([['apiBase', 'https://hub.example'], ['deviceToken', 'old-token'], ['deviceName', 'test-device'], ['foregroundOnManual', false]]);
  const session = sessionState || new Map();
  const tabs = tabsState || new Map();
  let listener;
  let sessionSetCount = 0;
  let resolveSessionSetStarted;
  const sessionSetStarted = new Promise((resolve) => { resolveSessionSetStarted = resolve; });
  let releaseSessionSet;
  const firstSessionSet = new Promise((resolve) => { releaseSessionSet = resolve; });
  const chrome = {
    storage: {
      local: {
        get: async (defaults) => Object.fromEntries(Object.keys(defaults).map((key) => [key, local.has(key) ? local.get(key) : defaults[key]])),
        set: async (values) => { for (const [key, value] of Object.entries(values)) local.set(key, value); },
        remove: async (keys) => { for (const key of (Array.isArray(keys) ? keys : [keys])) local.delete(key); },
      },
      session: {
        get: async (defaults) => Object.fromEntries(Object.keys(defaults).map((key) => [key, session.has(key) ? session.get(key) : defaults[key]])),
        set: async (values) => {
          sessionSetCount += 1;
          if (gateFirstSessionSet && sessionSetCount === 1) {
            resolveSessionSetStarted();
            await firstSessionSet;
          }
          for (const [key, value] of Object.entries(values)) session.set(key, value);
        },
        remove: async (keys) => { for (const key of (Array.isArray(keys) ? keys : [keys])) session.delete(key); },
      },
    },
    alarms: { create: async () => undefined, clear: async () => true, onAlarm: { addListener: () => undefined } },
    runtime: {
      onInstalled: { addListener: () => undefined },
      onStartup: { addListener: () => undefined },
      onMessage: { addListener: (callback) => { listener = callback; } },
    },
    tabs: {
      create: async ({ url }) => { const tab = { id: 7, url, status: 'complete', windowId: 1 }; tabs.set(tab.id, tab); return tab; },
      get: async (id) => { if (!tabs.has(id)) throw new Error('tab missing'); return tabs.get(id); },
      sendMessage: async (id, message) => message.type === 'apihub:inspect' ? inspectImpl(id, message) : true,
      remove: async (id) => { tabs.delete(id); },
      update: async (id, values) => { Object.assign(tabs.get(id), values); },
    },
    windows: { update: async () => undefined },
    action: { setBadgeText: async () => undefined, setBadgeBackgroundColor: async () => undefined },
  };
  const context = {
    chrome,
    self: { ApiHubUrl: require('./url.js') },
    importScripts: () => undefined,
    fetch: fetchImpl,
    Headers,
    AbortController,
    URL,
    Date,
    Promise,
    setTimeout,
    clearTimeout,
    console,
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'background.js'), 'utf8'), context, { filename: 'background.js' });
  return {
    local,
    session,
    tabs,
    waitForFirstSessionSet: () => sessionSetStarted,
    releaseFirstSessionSet: releaseSessionSet,
    send: (message) => new Promise((resolve) => listener(message, {}, resolve)),
  };
}

test('APIHub base requires HTTPS except for loopback development', () => {
  assert.equal(normalizeApiBase('https://APIHub.example/path/?token=ignored#fragment'), 'https://apihub.example/path');
  assert.equal(normalizeApiBase('http://127.0.0.1:4180/'), 'http://127.0.0.1:4180');
  assert.equal(normalizeApiBase('http://127.42.0.9:4180'), 'http://127.42.0.9:4180');
  assert.equal(normalizeApiBase('http://[::1]:4180'), 'http://[::1]:4180');
  assert.throws(() => normalizeApiBase('http://api.example'), /HTTPS/);
  assert.throws(() => normalizeApiBase('http://192.168.1.2'), /HTTPS/);
  assert.throws(() => normalizeApiBase('https://user:secret@api.example'), /用户名或密码/);
  assert.throws(() => normalizeApiBase('file:///tmp/apihub'), /HTTPS/);
});

test('manifest and page agent preserve the browser identity boundary', () => {
  const root = __dirname;
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8'));
  for (const permission of ['cookies', 'webRequest', 'scripting']) {
    assert.ok(!manifest.permissions.includes(permission), `forbidden permission: ${permission}`);
  }
  const source = fs.readFileSync(path.join(root, 'page-agent.js'), 'utf8');
  for (const pattern of [/document\.body/, /\.innerText/, /document\.cookie/, /localStorage/, /sessionStorage/]) {
    assert.doesNotMatch(source, pattern);
  }
  assert.match(source, /MAX_ELEMENTS/);
  assert.match(source, /MAX_ELEMENT_TEXT/);
});

test('background persists one active run across service worker restarts', () => {
  const source = fs.readFileSync(path.join(__dirname, 'background.js'), 'utf8');
  assert.match(source, /chrome\.storage\.session/);
  assert.match(source, /pollPromise/);
  assert.match(source, /loadRunState\(\)/);
  assert.match(source, /while \(Date\.now\(\) < deadline\)/);
  assert.match(source, /chrome\.tabs\.get\(tabId\)/);
});

test('disconnect waits for an active run before replacing its credentials', async () => {
  const task = { id: 'task-1', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-1' };
  const resultRequests = [];
  const harness = loadBackground({
    gateFirstSessionSet: true,
    fetchImpl: async (url, options = {}) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith('/claims')) return response({ data: task });
      if (pathname.endsWith('/results')) {
        resultRequests.push({ authorization: options.headers.get('Authorization'), body: JSON.parse(options.body) });
        return response({ data: task });
      }
      throw new Error(`unexpected request ${pathname}`);
    },
  });
  const poll = harness.send({ type: 'apihub:poll' });
  await harness.waitForFirstSessionSet();
  const disconnect = harness.send({ type: 'apihub:disconnect' });
  harness.releaseFirstSessionSet();
  assert.equal((await poll).ok, true);
  assert.equal((await disconnect).ok, true);
  assert.deepEqual(resultRequests, [{ authorization: 'Bearer old-token', body: { leaseToken: 'lease-1', status: 'failed', message: '设备已在本机断开连接' } }]);
  assert.equal(harness.local.has('deviceToken'), false);
  assert.equal(harness.session.has('apihub-companion-active-run'), false);
});

test('disconnect waits for an in-flight claim before clearing credentials', async () => {
  const task = { id: 'task-claim', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-claim' };
  let claimStarted;
  let releaseClaim;
  const claimGate = new Promise((resolve) => { releaseClaim = resolve; });
  const resultRequests = [];
  const harness = loadBackground({
    fetchImpl: async (url, options = {}) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith('/claims')) {
        claimStarted?.();
        await claimGate;
        return response({ data: task });
      }
      if (pathname.endsWith('/heartbeats')) return response({ data: task });
      if (pathname.endsWith('/results')) {
        resultRequests.push(options.headers.get('Authorization'));
        return response({ data: task });
      }
      throw new Error(`unexpected request ${pathname}`);
    },
  });
  const claimStartedPromise = new Promise((resolve) => { claimStarted = resolve; });
  const poll = harness.send({ type: 'apihub:poll' });
  await claimStartedPromise;
  const disconnect = harness.send({ type: 'apihub:disconnect' });
  releaseClaim();
  assert.equal((await poll).ok, true);
  assert.equal((await disconnect).ok, true);
  assert.deepEqual(resultRequests, ['Bearer old-token']);
  assert.equal(harness.local.has('deviceToken'), false);
});

test('result reporting remains durable across transient network failures', async () => {
  const task = { id: 'task-2', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-2' };
  let phase = 'offline';
  let resultAttempts = 0;
  const fetchImpl = async (url, options = {}) => {
    const pathname = new URL(url).pathname;
    if (pathname.endsWith('/claims')) return response({ data: task });
    if (pathname.endsWith('/heartbeats')) return response({ data: task });
    if (pathname.endsWith('/results')) {
      resultAttempts += 1;
      if (phase === 'offline') throw new TypeError('network unavailable');
      assert.equal(options.headers.get('Authorization'), 'Bearer old-token');
      return response({ data: task });
    }
    throw new Error(`unexpected request ${pathname}`);
  };
  const harness = loadBackground({ fetchImpl });
  assert.equal((await harness.send({ type: 'apihub:poll' })).ok, true);
  assert.equal(resultAttempts, 3);
  assert.equal(harness.session.get('apihub-companion-active-run').pendingResult.status, 'success');
  assert.equal(harness.tabs.size, 1);
  phase = 'online';
  const restarted = loadBackground({
    fetchImpl,
    localState: harness.local,
    sessionState: harness.session,
    tabsState: harness.tabs,
  });
  assert.equal((await restarted.send({ type: 'apihub:poll' })).ok, true);
  assert.equal(resultAttempts, 4);
  assert.equal(restarted.session.has('apihub-companion-active-run'), false);
  assert.equal(restarted.tabs.size, 0);
});

test('terminal failures close their task tab after the result is accepted', async () => {
  const task = { id: 'task-failed', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-failed' };
  const results = [];
  const harness = loadBackground({
    fetchImpl: async (url, options = {}) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith('/claims')) return response({ data: task });
      if (pathname.endsWith('/heartbeats')) return response({ error: { message: 'lease rejected' } }, 409);
      if (pathname.endsWith('/results')) {
        results.push(JSON.parse(options.body));
        return response({ data: task });
      }
      throw new Error(`unexpected request ${pathname}`);
    },
  });
  assert.equal((await harness.send({ type: 'apihub:poll' })).ok, true);
  assert.equal(results[0].status, 'failed');
  assert.equal(harness.session.has('apihub-companion-active-run'), false);
  assert.equal(harness.tabs.size, 0);
});

test('recovered manual results keep the task tab for the user', async () => {
  const task = { id: 'task-manual', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-manual' };
  const localState = new Map([['apiBase', 'https://hub.example'], ['deviceToken', 'old-token'], ['deviceName', 'test-device'], ['foregroundOnManual', false]]);
  const sessionState = new Map([['apihub-companion-active-run', {
    task,
    tabId: 7,
    actedUrl: task.targetUrl,
    manualSeen: true,
    deadlineAt: Date.now() + 60_000,
    pendingResult: { status: 'manual_required', message: 'complete verification' },
  }]]);
  const tabsState = new Map([[7, { id: 7, url: task.targetUrl, status: 'complete', windowId: 1 }]]);
  const harness = loadBackground({
    localState,
    sessionState,
    tabsState,
    fetchImpl: async (url) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith('/results')) return response({ data: task });
      throw new Error(`unexpected request ${pathname}`);
    },
  });
  assert.equal((await harness.send({ type: 'apihub:poll' })).ok, true);
  assert.equal(harness.session.has('apihub-companion-active-run'), false);
  assert.equal(harness.tabs.size, 1);
});

test('pending results block disconnect and pairing without losing old credentials', async () => {
  const task = { id: 'task-3', targetUrl: 'https://site.example/checkin', leaseToken: 'lease-3' };
  let pairingRequests = 0;
  let resultAttempts = 0;
  const harness = loadBackground({
    fetchImpl: async (url) => {
      const pathname = new URL(url).pathname;
      if (pathname.endsWith('/heartbeats')) return response({ data: task });
      if (pathname.endsWith('/results')) {
        resultAttempts += 1;
        throw new TypeError('network unavailable');
      }
      if (pathname.endsWith('/pairings')) {
        pairingRequests += 1;
        return response({ data: { deviceToken: 'new-token', device: { id: 'new-device', name: 'new' } } });
      }
      throw new Error(`unexpected request ${pathname}`);
    },
  });
  harness.session.set('apihub-companion-active-run', {
    task,
    actedUrl: task.targetUrl,
    manualSeen: false,
    deadlineAt: Date.now() + 60_000,
    pendingResult: { status: 'success', message: 'completed' },
  });
  const disconnect = await harness.send({ type: 'apihub:disconnect' });
  assert.equal(disconnect.ok, false);
  assert.equal(harness.local.get('deviceToken'), 'old-token');
  const pairing = await harness.send({ type: 'apihub:pair', apiBase: 'https://new.example', code: 'one-time-code', deviceName: 'new' });
  assert.equal(pairing.ok, false);
  assert.equal(pairingRequests, 0);
  assert.equal(resultAttempts, 6);
  assert.equal(harness.local.get('deviceToken'), 'old-token');
  assert.equal(harness.session.get('apihub-companion-active-run').pendingResult.status, 'success');
});
