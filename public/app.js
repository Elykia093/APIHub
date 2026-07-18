const state = {
  token: sessionStorage.getItem('apihub-admin-token') || '',
  sites: [],
};

const elements = {
  loginPanel: document.querySelector('#login-panel'),
  appPanel: document.querySelector('#app-panel'),
  loginForm: document.querySelector('#login-form'),
  tokenInput: document.querySelector('#admin-token'),
  loginError: document.querySelector('#login-error'),
  connection: document.querySelector('#connection-state'),
  logout: document.querySelector('#logout-button'),
  siteForm: document.querySelector('#site-form'),
  showSiteForm: document.querySelector('#show-site-form'),
  cancelSiteForm: document.querySelector('#cancel-site-form'),
  siteFormError: document.querySelector('#site-form-error'),
  sitesList: document.querySelector('#sites-list'),
  checkinsBody: document.querySelector('#checkins-body'),
  announcementsList: document.querySelector('#announcements-list'),
  refresh: document.querySelector('#refresh-button'),
  toast: document.querySelector('#toast'),
};

function el(tag, options = {}) {
  const node = document.createElement(tag);
  if (options.className) node.className = options.className;
  if (options.text !== undefined) node.textContent = String(options.text);
  if (options.type) node.type = options.type;
  return node;
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => { elements.toast.hidden = true; }, 3500);
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set('Authorization', `Bearer ${state.token}`);
  if (options.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const response = await fetch(path, { ...options, headers });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401) logout();
    throw new Error(payload?.error?.message || `HTTP ${response.status}`);
  }
  return payload;
}

function setAuthenticated(authenticated) {
  elements.loginPanel.hidden = authenticated;
  elements.appPanel.hidden = !authenticated;
  elements.logout.hidden = !authenticated;
  elements.connection.textContent = authenticated ? '已连接' : '未连接';
  elements.connection.classList.toggle('online', authenticated);
}

function logout() {
  state.token = '';
  state.sites = [];
  sessionStorage.removeItem('apihub-admin-token');
  setAuthenticated(false);
}

function statusLabel(status) {
  const labels = {
    success: '成功',
    already_checked: '今日已签到',
    manual_required: '需人工处理',
    failed: '失败',
    running: '执行中',
    skipped: '已跳过',
  };
  return labels[status] || status;
}

function button(text, className, handler) {
  const node = el('button', { text, className: `button ${className}`, type: 'button' });
  node.addEventListener('click', async () => {
    node.disabled = true;
    try {
      await handler();
    } catch (error) {
      showToast(error instanceof Error ? error.message : '操作失败');
    } finally {
      node.disabled = false;
    }
  });
  return node;
}

function renderSites(sites) {
  elements.sitesList.replaceChildren();
  if (sites.length === 0) {
    elements.sitesList.append(el('div', { className: 'empty-state', text: '还没有站点，先添加一个公益站。' }));
    return;
  }
  for (const site of sites) {
    const card = el('article', { className: 'site-card' });
    const head = el('div', { className: 'site-card-head' });
    const title = el('div');
    title.append(el('h3', { text: site.name }), el('div', { className: 'site-url', text: site.baseUrl }));
    head.append(title, el('span', { className: `tag ${site.enabled ? 'good' : 'warn'}`, text: site.enabled ? '运行中' : '已停用' }));

    const meta = el('div', { className: 'site-meta' });
    meta.append(
      el('span', { className: 'tag', text: site.adapter }),
      el('span', { className: 'tag', text: `签到 ${site.checkinCron}` }),
      el('span', { className: 'tag', text: `公告 ${site.announcementCron}` }),
      el('span', { className: 'tag', text: site.timezone }),
      el('span', { className: site.consecutiveFailures ? 'tag warn' : 'tag good', text: `连续失败 ${site.consecutiveFailures}` }),
    );

    const actions = el('div', { className: 'site-actions' });
    if (site.capabilities?.checkin && site.checkinEnabled) {
      actions.append(button('立即签到', 'button-primary', async () => {
        const payload = await api(`/api/v1/sites/${site.id}/checkin-runs`, { method: 'POST' });
        showToast(`${site.name}：${statusLabel(payload.data.status)} · ${payload.data.message || '完成'}`);
        await loadAll();
      }));
    }
    if (site.capabilities?.announcements && site.announcementEnabled) {
      actions.append(button('同步公告', 'button-secondary', async () => {
        const payload = await api(`/api/v1/sites/${site.id}/announcement-syncs`, { method: 'POST' });
        showToast(`${site.name}：新增 ${payload.data.addedCount} 条公告`);
        await loadAll();
      }));
    }
    actions.append(
      button(site.enabled ? '停用' : '启用', site.enabled ? 'button-danger' : 'button-secondary', async () => {
        await api(`/api/v1/sites/${site.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !site.enabled }) });
        await loadAll();
      }),
    );
    card.append(head, meta, actions);
    elements.sitesList.append(card);
  }
}

function renderCheckins(checkins) {
  elements.checkinsBody.replaceChildren();
  if (checkins.length === 0) {
    const row = el('tr');
    const cell = el('td', { text: '暂无签到记录' });
    cell.colSpan = 5;
    row.append(cell);
    elements.checkinsBody.append(row);
    return;
  }
  for (const run of checkins) {
    const row = el('tr');
    row.append(
      el('td', { text: run.siteName || run.siteId }),
      el('td', { text: run.localDate }),
      el('td', { text: statusLabel(run.status) }),
      el('td', { text: run.rewardValue ?? '—' }),
      el('td', { text: run.message || '—' }),
    );
    elements.checkinsBody.append(row);
  }
}

function renderAnnouncements(items) {
  elements.announcementsList.replaceChildren();
  if (items.length === 0) {
    elements.announcementsList.append(el('div', { className: 'empty-state', text: '暂无公告，选择一个站点同步。' }));
    return;
  }
  for (const item of items) {
    const card = el('article', { className: `announcement-card${item.readAt ? ' read' : ''}` });
    const head = el('div', { className: 'announcement-head' });
    const meta = el('small', { text: `${item.siteName || item.siteId} · ${item.source === 'notice' ? '通知' : '公告'}${item.publishedAt ? ` · ${new Date(item.publishedAt).toLocaleString()}` : ''}` });
    head.append(meta);
    if (!item.readAt) {
      head.append(button('标为已读', 'button-secondary', async () => {
        await api(`/api/v1/announcements/${item.id}`, { method: 'PATCH', body: JSON.stringify({ read: true }) });
        await loadAll();
      }));
    }
    card.append(head, el('p', { text: item.content }));
    if (item.extra) card.append(el('small', { text: item.extra }));
    elements.announcementsList.append(card);
  }
}

async function loadAll() {
  const [summary, sites, checkins, announcements] = await Promise.all([
    api('/api/v1/summary'),
    api('/api/v1/sites'),
    api('/api/v1/checkin-runs?limit=30'),
    api('/api/v1/announcements?limit=30'),
  ]);
  state.sites = sites.data;
  document.querySelector('#metric-sites').textContent = summary.sites.total;
  document.querySelector('#metric-sites-detail').textContent = `${summary.sites.enabled} 个启用`;
  const completed = (summary.today.success || 0) + (summary.today.already_checked || 0);
  document.querySelector('#metric-checkins').textContent = completed;
  document.querySelector('#metric-checkins-detail').textContent = `${summary.today.failed || 0} 个失败，${summary.today.manual_required || 0} 个需人工`;
  document.querySelector('#metric-announcements').textContent = summary.unreadAnnouncements;
  renderSites(sites.data);
  renderCheckins(checkins.data);
  renderAnnouncements(announcements.data);
}

elements.loginForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  elements.loginError.textContent = '';
  state.token = elements.tokenInput.value;
  try {
    await loadAll();
    sessionStorage.setItem('apihub-admin-token', state.token);
    elements.tokenInput.value = '';
    setAuthenticated(true);
  } catch (error) {
    elements.loginError.textContent = error instanceof Error ? error.message : '连接失败';
    logout();
  }
});

elements.logout.addEventListener('click', logout);
elements.refresh.addEventListener('click', async () => {
  try { await loadAll(); showToast('已刷新'); } catch (error) { showToast(error.message || '刷新失败'); }
});
elements.showSiteForm.addEventListener('click', () => { elements.siteForm.hidden = false; });
elements.cancelSiteForm.addEventListener('click', () => { elements.siteForm.hidden = true; elements.siteForm.reset(); });

elements.siteForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  elements.siteFormError.textContent = '';
  const form = new FormData(elements.siteForm);
  const body = Object.fromEntries(form.entries());
  try {
    await api('/api/v1/sites', { method: 'POST', body: JSON.stringify(body) });
    elements.siteForm.reset();
    elements.siteForm.hidden = true;
    showToast('站点已添加');
    await loadAll();
  } catch (error) {
    elements.siteFormError.textContent = error instanceof Error ? error.message : '保存失败';
  }
});

if (state.token) {
  loadAll().then(() => setAuthenticated(true)).catch(() => logout());
} else {
  setAuthenticated(false);
}
