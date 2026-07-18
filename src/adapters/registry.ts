import type { SiteAdapter, AdapterSiteContext } from './types.js';
import type { SiteAdapterName, SiteRecord } from '../domain/types.js';
import { AppError } from '../lib/errors.js';
import type { CredentialVault } from '../security/crypto.js';

export type SiteAdapterDescriptor = {
  name: SiteAdapterName;
  displayName: string;
  capabilities: SiteAdapter['capabilities'];
};

export class AdapterRegistry {
  readonly #adapters = new Map<SiteAdapterName, SiteAdapter>();

  constructor(adapters: SiteAdapter[]) {
    for (const adapter of adapters) this.#adapters.set(adapter.name, adapter);
  }

  get(name: SiteAdapterName): SiteAdapter {
    const adapter = this.#adapters.get(name);
    if (!adapter) throw new AppError(422, 'VALIDATION_ERROR', `Unsupported site adapter: ${name}`);
    return adapter;
  }

  describe(name: SiteAdapterName): SiteAdapterDescriptor {
    const adapter = this.get(name);
    return {
      name: adapter.name,
      displayName: adapter.displayName,
      capabilities: { ...adapter.capabilities },
    };
  }

  list(): SiteAdapterDescriptor[] {
    return [...this.#adapters.values()].map((adapter) => this.describe(adapter.name));
  }

  context(site: SiteRecord, vault: CredentialVault): AdapterSiteContext {
    return {
      baseUrl: site.baseUrl,
      userId: site.userId,
      accessToken: vault.decrypt(site.accessTokenCiphertext),
      timezone: site.timezone,
    };
  }
}
