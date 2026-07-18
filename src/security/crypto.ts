import { createCipheriv, createDecipheriv, createHash, randomBytes, timingSafeEqual } from 'node:crypto';

const CIPHER = 'aes-256-gcm';
const VERSION = 'v1';

export class CredentialVault {
  readonly #key: Buffer;

  constructor(secret: string) {
    this.#key = createHash('sha256').update(secret, 'utf8').digest();
  }

  encrypt(value: string): string {
    const iv = randomBytes(12);
    const cipher = createCipheriv(CIPHER, this.#key, iv);
    const encrypted = Buffer.concat([cipher.update(value, 'utf8'), cipher.final()]);
    const tag = cipher.getAuthTag();
    return [VERSION, iv.toString('base64url'), tag.toString('base64url'), encrypted.toString('base64url')].join(':');
  }

  decrypt(value: string): string {
    const parts = value.split(':');
    if (parts.length !== 4 || parts[0] !== VERSION) {
      throw new Error('Unsupported encrypted credential format');
    }

    const iv = Buffer.from(parts[1] ?? '', 'base64url');
    const tag = Buffer.from(parts[2] ?? '', 'base64url');
    const encrypted = Buffer.from(parts[3] ?? '', 'base64url');
    const decipher = createDecipheriv(CIPHER, this.#key, iv);
    decipher.setAuthTag(tag);
    return Buffer.concat([decipher.update(encrypted), decipher.final()]).toString('utf8');
  }
}

export function tokensEqual(candidate: string, expected: string): boolean {
  const candidateDigest = createHash('sha256').update(candidate, 'utf8').digest();
  const expectedDigest = createHash('sha256').update(expected, 'utf8').digest();
  return timingSafeEqual(candidateDigest, expectedDigest);
}
