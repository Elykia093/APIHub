(function exposeApiHubUrl(global) {
  function isLoopbackHostname(hostname) {
    const host = hostname.toLowerCase();
    if (host === 'localhost' || host === '[::1]' || host === '::1') return true;
    const parts = host.split('.');
    return parts.length === 4
      && parts[0] === '127'
      && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
  }

  function normalizeApiBase(value) {
    let url;
    try {
      url = new URL(String(value || '').trim());
    } catch {
      throw new Error('APIHub 地址必须是有效 URL');
    }
    if (url.username || url.password) {
      throw new Error('APIHub 地址不能包含用户名或密码');
    }
    if (url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopbackHostname(url.hostname))) {
      throw new Error('APIHub 地址必须使用 HTTPS；本地开发仅允许 loopback HTTP');
    }
    url.hash = '';
    url.search = '';
    url.pathname = url.pathname.replace(/\/+$/, '');
    return url.toString().replace(/\/+$/, '');
  }

  function apiUrl(base, path) {
    return `${base.replace(/\/+$/, '')}${path}`;
  }

  const api = { apiUrl, isLoopbackHostname, normalizeApiBase };
  global.ApiHubUrl = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
}(typeof self !== 'undefined' ? self : globalThis));
