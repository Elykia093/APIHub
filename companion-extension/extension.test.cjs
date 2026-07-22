const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const { normalizeApiBase } = require('./url.js');

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
