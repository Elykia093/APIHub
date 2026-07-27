(() => {
  const MAX_ELEMENTS = 160;
  const MAX_ELEMENT_TEXT = 500;
  const MAX_TEXT_NODES = 12;
  const MAX_PAGE_TEXT = 20_000;
  const SEMANTIC_SELECTOR = 'h1, h2, h3, p, li, dt, dd, label, button, [role="button"], [role="alert"], [role="status"], output, [aria-label], [class*="balance" i], [class*="credit" i]';
  const compact = (value) => String(value || '').replace(/\s+/g, ' ').trim();
  const visible = (element) => {
    const style = getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const elementText = (element) => {
    const aria = element.getAttribute('aria-label');
    if (aria) return compact(aria.slice(0, MAX_ELEMENT_TEXT));
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    let value = '';
    let count = 0;
    while (count < MAX_TEXT_NODES && value.length < MAX_ELEMENT_TEXT) {
      const node = walker.nextNode();
      if (!node) break;
      value += ` ${String(node.nodeValue || '').slice(0, MAX_ELEMENT_TEXT - value.length)}`;
      count += 1;
    }
    return compact(value);
  };
  const pageText = () => {
    const parts = [];
    let length = 0;
    let count = 0;
    for (const element of document.querySelectorAll(SEMANTIC_SELECTOR)) {
      if (count >= MAX_ELEMENTS || length >= MAX_PAGE_TEXT) break;
      count += 1;
      if (!visible(element)) continue;
      const value = elementText(element).slice(0, MAX_PAGE_TEXT - length);
      if (!value) continue;
      parts.push(value);
      length += value.length;
    }
    return parts.join(' ');
  };
  const matching = (selector, pattern) => {
    let count = 0;
    for (const element of document.querySelectorAll(selector)) {
      if (count >= MAX_ELEMENTS) break;
      count += 1;
      if (visible(element) && pattern.test(elementText(element))) return element;
    }
    return null;
  };
  const balance = (text) => {
    const match = text.match(/(?:账户余额|账号余额|当前余额|剩余余额|余额|Balance|Credit|Credits)\s*[:：]?\s*([$¥￥]?\s*[-+]?\d+(?:,\d{3})*(?:\.\d+)?)/i);
    return match?.[1] ? compact(match[1]).slice(0, 128) : null;
  };
  const inspect = (act) => {
    const text = pageText();
    const challenge = document.querySelector('iframe[src*="turnstile" i], iframe[src*="captcha" i], .cf-turnstile, [class*="captcha" i], [id*="captcha" i]') || /turnstile|人机验证|安全验证|完成验证|verify you are human|checking your browser/i.test(text);
    if (challenge) return { state: 'manual_required', message: '等待用户完成人机验证' };
    if (/今日已签到|已经签到|已签到|already checked|checked in today/i.test(text)) return { state: 'already_checked', message: '页面显示今日已签到', balance: balance(text) };
    if (/签到成功|check(?:ed)? in successfully|successfully checked/i.test(text)) return { state: 'success', message: '页面显示签到成功', balance: balance(text) };
    const checkin = matching('button, [role="button"], input[type="button"], input[type="submit"], a', /^(?:立即签到|签到|签\s*到|check\s*in)$/i);
    if (checkin) {
      if (checkin.disabled || checkin.getAttribute('aria-disabled') === 'true') return { state: 'already_checked', message: '签到按钮已不可用', balance: balance(text) };
      if (act) checkin.click();
      return { state: act ? 'action' : 'ready', message: act ? '已点击签到按钮' : '找到签到按钮' };
    }
    const oauth = matching('a, button, [role="button"]', /linux\.do|github|oauth|第三方登录|授权登录/i);
    const loginPage = document.querySelector('input[type="password"]') || /登录|sign\s*in|log\s*in/i.test(document.title) || /\/(?:login|signin|sign-in)(?:\/|$)/i.test(location.pathname);
    if (oauth && loginPage) {
      if (act) oauth.click();
      return { state: act ? 'action' : 'manual_required', message: act ? '已触发浏览器 OAuth 登录' : '等待用户完成登录' };
    }
    if (loginPage) return { state: 'manual_required', message: '等待用户完成站点登录' };
    return { state: 'waiting', message: '等待页面出现签到状态或按钮', balance: balance(text) };
  };
  chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
    if (message?.type === 'apihub:inspect') {
      try { sendResponse(inspect(message.act === true)); } catch { sendResponse({ state: 'waiting', message: '页面尚未准备完成' }); }
      return;
    }
    if (message?.type !== 'apihub:wait') return;
    const observer = new MutationObserver(() => { observer.disconnect(); clearTimeout(timer); sendResponse(true); });
    const timer = setTimeout(() => { observer.disconnect(); sendResponse(false); }, 15_000);
    observer.observe(document.documentElement, { childList: true, subtree: true, attributes: true });
    return true;
  });
})();
