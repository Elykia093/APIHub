import { createHash } from 'node:crypto';
import type { FastifyBaseLogger } from 'fastify';
import type { AppDatabase } from '../db/database.js';
import type { AdapterRegistry } from '../adapters/registry.js';
import type { CredentialVault } from '../security/crypto.js';
import type { AnnouncementSyncRun } from '../domain/types.js';
import { AppError, asAppError } from '../lib/errors.js';

function fingerprint(content: string): string {
  return createHash('sha256')
    .update(content, 'utf8')
    .digest('hex');
}

export class AnnouncementService {
  readonly #activeSites = new Set<string>();

  constructor(
    private readonly db: AppDatabase,
    private readonly adapters: AdapterRegistry,
    private readonly vault: CredentialVault,
    private readonly logger: FastifyBaseLogger,
  ) {}

  async sync(siteId: string, requestId: string): Promise<AnnouncementSyncRun> {
    if (this.#activeSites.has(siteId)) {
      throw new AppError(409, 'CONFLICT', 'An announcement sync is already running for this site', true);
    }
    this.#activeSites.add(siteId);
    try {
      const site = await this.db.getSiteOrThrow(siteId);
      if (!site.enabled || !site.announcementEnabled) {
        throw new AppError(409, 'CONFLICT', 'Announcement sync is disabled for this site');
      }
      const run = await this.db.createAnnouncementSyncRun(site.id, requestId);
      try {
        const adapter = this.adapters.get(site.adapter);
        if (!adapter.fetchAnnouncements) {
          throw new AppError(409, 'CONFLICT', `${adapter.displayName} does not support announcement sync`);
        }
        const result = await adapter.fetchAnnouncements(this.adapters.context(site, this.vault));
        let addedCount = 0;
        for (const item of result.items) {
          const inserted = await this.db.upsertAnnouncement({
            siteId: site.id,
            ...item,
            fingerprint: fingerprint(item.content),
          });
          if (inserted) addedCount += 1;
        }
        return this.db.finishAnnouncementSyncRun(run.id, {
          status: result.warnings.length > 0 ? 'partial' : 'success',
          addedCount,
          message: result.warnings.join('; '),
        });
      } catch (error) {
        const appError = asAppError(error);
        this.logger.warn({
          siteId: site.id,
          requestId,
          errorCode: appError.code,
          retryable: appError.retryable,
        }, 'announcement sync failed');
        return this.db.finishAnnouncementSyncRun(run.id, {
          status: 'failed',
          addedCount: 0,
          message: appError.message,
        });
      }
    } finally {
      this.#activeSites.delete(siteId);
    }
  }
}
