import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeBaseUrl } from '../dist/security/safe-http.js';

test('built ESM blocks private literals through ipaddr.js', () => {
  assert.throws(
    () => normalizeBaseUrl('https://127.0.0.1', false, false),
    (error) => error?.code === 'SITE_URL_BLOCKED',
  );
  assert.equal(
    normalizeBaseUrl('https://8.8.8.8/path', false, false),
    'https://8.8.8.8/path',
  );
});
