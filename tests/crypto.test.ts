import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { CredentialVault, tokensEqual } from '../src/security/crypto.js';
import { APP_SECRET } from './helpers.js';

type CompatibilityVectors = {
  aes256Gcm: {
    secretUtf8: string;
    plaintextUtf8: string;
    ivHex: string;
    ivBase64Url: string;
    tagHex: string;
    tagBase64Url: string;
    ciphertextHex: string;
    ciphertextBase64Url: string;
    serialized: string;
  };
};

const { aes256Gcm } = JSON.parse(
  readFileSync(new URL('./fixtures/compatibility-vectors.json', import.meta.url), 'utf8'),
) as CompatibilityVectors;

describe('CredentialVault', () => {
  it('encrypts with authenticated encryption and decrypts the original value', () => {
    const vault = new CredentialVault(APP_SECRET);
    const plaintext = 'secret-station-token';
    const first = vault.encrypt(plaintext);
    const second = vault.encrypt(plaintext);

    expect(first).not.toContain(plaintext);
    expect(first).not.toBe(second);
    expect(vault.decrypt(first)).toBe(plaintext);
    expect(vault.decrypt(second)).toBe(plaintext);
  });

  it('rejects tampered ciphertext', () => {
    const vault = new CredentialVault(APP_SECRET);
    const encrypted = vault.encrypt('secret-station-token');
    const [version, iv, tag, ciphertext] = encrypted.split(':');
    const tamperedBytes = Buffer.from(ciphertext!, 'base64url');
    tamperedBytes[0] = tamperedBytes[0]! ^ 1;
    const tampered = [version, iv, tag, tamperedBytes.toString('base64url')].join(':');

    expect(() => vault.decrypt(tampered)).toThrow();
  });

  it('decrypts the language-neutral AES-GCM compatibility vector', () => {
    const vault = new CredentialVault(aes256Gcm.secretUtf8);
    const [version, iv, tag, ciphertext] = aes256Gcm.serialized.split(':');

    expect(version).toBe('v1');
    expect(iv).toBe(aes256Gcm.ivBase64Url);
    expect(tag).toBe(aes256Gcm.tagBase64Url);
    expect(ciphertext).toBe(aes256Gcm.ciphertextBase64Url);
    expect(Buffer.from(iv!, 'base64url').toString('hex')).toBe(aes256Gcm.ivHex);
    expect(Buffer.from(tag!, 'base64url').toString('hex')).toBe(aes256Gcm.tagHex);
    expect(Buffer.from(ciphertext!, 'base64url').toString('hex')).toBe(aes256Gcm.ciphertextHex);
    expect(vault.decrypt(aes256Gcm.serialized)).toBe(aes256Gcm.plaintextUtf8);
  });
});

describe('tokensEqual', () => {
  it('compares equal and unequal tokens without length-dependent early returns', () => {
    expect(tokensEqual('same-token', 'same-token')).toBe(true);
    expect(tokensEqual('short', 'a-much-longer-token')).toBe(false);
  });
});
