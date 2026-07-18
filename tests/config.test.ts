import { describe, expect, it } from 'vitest';
import { loadConfig } from '../src/config.js';
import { ADMIN_TOKEN, APP_SECRET } from './helpers.js';

describe('loadConfig', () => {
  it('loads deployment environment values and parses explicit booleans', () => {
    const config = loadConfig({
      NODE_ENV: 'production',
      HOST: '0.0.0.0',
      PORT: '5180',
      DATABASE_URL: 'postgresql://apihub:secret@postgres:5432/apihub',
      DATABASE_POOL_MAX: '7',
      ADMIN_TOKEN,
      APP_SECRET,
      ALLOW_PRIVATE_SITES: 'true',
      ALLOW_INSECURE_HTTP: 'false',
    });

    expect(config).toMatchObject({
      nodeEnv: 'production',
      host: '0.0.0.0',
      port: 5180,
      databaseUrl: 'postgresql://apihub:secret@postgres:5432/apihub',
      databasePoolMax: 7,
      adminToken: ADMIN_TOKEN,
      appSecret: APP_SECRET,
      allowPrivateSites: true,
      allowInsecureHttp: false,
    });
  });

  it('refuses to start without required secrets', () => {
    expect(() => loadConfig({})).toThrow();
  });

  it('rejects non-PostgreSQL database URLs', () => {
    expect(() => loadConfig({
      DATABASE_URL: 'mysql://user:secret@database/app',
      ADMIN_TOKEN,
      APP_SECRET,
    })).toThrow('DATABASE_URL must be a PostgreSQL connection URL');
  });

  it('keeps missing, empty, trimmed, and scientific environment coercion stable', () => {
    const base = {
      DATABASE_URL: 'postgresql://user:secret@database/app',
      ADMIN_TOKEN,
      APP_SECRET,
    };
    expect(loadConfig({ ...base, PORT: '1e3', DATABASE_POOL_MAX: '10.0' })).toMatchObject({
      port: 1000,
      databasePoolMax: 10,
    });
    expect(() => loadConfig({ ...base, PORT: '' })).toThrow();
    expect(() => loadConfig({ ...base, NODE_ENV: ' production ' })).toThrow();
    expect(() => loadConfig({ ...base, HOST: ' ' })).toThrow();
    expect(() => loadConfig({ ...base, ALLOW_PRIVATE_SITES: '' })).toThrow();
  });
});
