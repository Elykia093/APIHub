import { randomUUID } from 'node:crypto';
import cron, { type ScheduledTask } from 'node-cron';
import type { FastifyBaseLogger } from 'fastify';
import type { AppDatabase } from '../db/database.js';
import type { CheckinService } from './checkin-service.js';
import type { AnnouncementService } from './announcement-service.js';

export function assertValidCron(expression: string): void {
  if (!cron.validate(expression)) throw new Error('Invalid cron expression');
}

export function assertValidTimezone(timezone: string): void {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: timezone }).format();
  } catch {
    throw new Error('Invalid IANA timezone');
  }
}

export class SchedulerService {
  readonly #tasks: ScheduledTask[] = [];

  constructor(
    private readonly db: AppDatabase,
    private readonly checkins: CheckinService,
    private readonly announcements: AnnouncementService,
    private readonly logger: FastifyBaseLogger,
  ) {}

  async reload(): Promise<void> {
    this.stop();
    for (const site of await this.db.listSites()) {
      if (!site.enabled) continue;
      assertValidTimezone(site.timezone);
      if (site.checkinEnabled) {
        assertValidCron(site.checkinCron);
        this.#tasks.push(cron.schedule(site.checkinCron, async () => {
          const requestId = `scheduler:${randomUUID()}`;
          try {
            await this.checkins.run(site.id, requestId);
          } catch (error) {
            this.logger.error({ siteId: site.id, requestId, err: error }, 'scheduled check-in crashed');
          }
        }, { timezone: site.timezone, noOverlap: true, name: `checkin:${site.id}` }));
      }
      if (site.announcementEnabled) {
        assertValidCron(site.announcementCron);
        this.#tasks.push(cron.schedule(site.announcementCron, async () => {
          const requestId = `scheduler:${randomUUID()}`;
          try {
            await this.announcements.sync(site.id, requestId);
          } catch (error) {
            this.logger.error({ siteId: site.id, requestId, err: error }, 'scheduled announcement sync crashed');
          }
        }, { timezone: site.timezone, noOverlap: true, name: `announcements:${site.id}` }));
      }
    }
  }

  stop(): void {
    for (const task of this.#tasks.splice(0)) task.destroy();
  }
}
