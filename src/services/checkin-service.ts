import type { FastifyBaseLogger } from 'fastify';
import type { AppDatabase } from '../db/database.js';
import type { AdapterRegistry } from '../adapters/registry.js';
import type { CredentialVault } from '../security/crypto.js';
import type { CheckinRun } from '../domain/types.js';
import { AppError, asAppError } from '../lib/errors.js';
import { localDateForTimezone } from '../lib/time.js';

const terminalStatuses = new Set(['success', 'already_checked', 'manual_required']);

export class CheckinService {
  readonly #activeSites = new Set<string>();

  constructor(
    private readonly db: AppDatabase,
    private readonly adapters: AdapterRegistry,
    private readonly vault: CredentialVault,
    private readonly logger: FastifyBaseLogger,
  ) {}

  async run(siteId: string, requestId: string, now = new Date()): Promise<CheckinRun> {
    if (this.#activeSites.has(siteId)) {
      throw new AppError(409, 'CONFLICT', 'A check-in is already running for this site', true);
    }
    this.#activeSites.add(siteId);
    try {
      const site = await this.db.getSiteOrThrow(siteId);
      if (!site.enabled || !site.checkinEnabled) {
        throw new AppError(409, 'CONFLICT', 'Check-in is disabled for this site');
      }

      const localDate = localDateForTimezone(now, site.timezone);
      const existing = await this.db.findCheckinForDate(site.id, localDate);
      if (existing && terminalStatuses.has(existing.status)) return existing;
      if (existing?.status === 'running') {
        throw new AppError(409, 'CONFLICT', 'A check-in is already running for this site', true);
      }

      const begun = await this.db.beginCheckin(site.id, localDate, requestId);
      if (!begun.acquired) {
        if (terminalStatuses.has(begun.run.status)) return begun.run;
        throw new AppError(409, 'CONFLICT', 'A check-in is already running for this site', true);
      }
      const run = begun.run;
      try {
        const adapter = this.adapters.get(site.adapter);
        if (!adapter.checkIn) {
          throw new AppError(409, 'CONFLICT', `${adapter.displayName} does not support server-side check-in`);
        }
        const result = await adapter.checkIn(this.adapters.context(site, this.vault), localDate);
        const finished = await this.db.finishCheckin(run.id, {
          status: result.status,
          rewardValue: result.rewardValue,
          message: result.message,
          errorCode: result.status === 'manual_required' ? 'MANUAL_ACTION_REQUIRED' : null,
        });
        await this.db.setSiteFailureCount(site.id, result.status === 'manual_required' ? 'increment' : 'reset');
        return finished;
      } catch (error) {
        const appError = asAppError(error);
        this.logger.warn({
          siteId: site.id,
          requestId,
          errorCode: appError.code,
          retryable: appError.retryable,
        }, 'check-in failed');
        const status = appError.code === 'MANUAL_ACTION_REQUIRED' ? 'manual_required' : 'failed';
        const finished = await this.db.finishCheckin(run.id, {
          status,
          message: appError.message,
          errorCode: appError.code,
        });
        await this.db.setSiteFailureCount(site.id, 'increment');
        return finished;
      }
    } finally {
      this.#activeSites.delete(siteId);
    }
  }
}
