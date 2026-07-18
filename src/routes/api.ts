import type { FastifyInstance } from 'fastify';
import { z, type ZodType } from 'zod';
import cron from 'node-cron';
import type { AppConfig } from '../config.js';
import type { AppDatabase } from '../db/database.js';
import {
  SITE_ADAPTER_NAMES,
  toPublicSite,
  type SiteAdapterName,
  type SiteAdapterSelection,
  type SiteRecord,
} from '../domain/types.js';
import { AppError } from '../lib/errors.js';
import type { AdapterRegistry } from '../adapters/registry.js';
import type { SiteAdapterDetector } from '../adapters/detector.js';
import type { CredentialVault } from '../security/crypto.js';
import { normalizeBaseUrl } from '../security/safe-http.js';
import type { CheckinService } from '../services/checkin-service.js';
import type { AnnouncementService } from '../services/announcement-service.js';
import { assertValidTimezone, type SchedulerService } from '../services/scheduler.js';

type RouteDependencies = {
  config: AppConfig;
  db: AppDatabase;
  vault: CredentialVault;
  adapters: AdapterRegistry;
  detector: SiteAdapterDetector;
  checkins: CheckinService;
  announcements: AnnouncementService;
  scheduler: SchedulerService;
};

const siteIdParams = z.object({ siteId: z.uuid() }).strict();
const announcementIdParams = z.object({ announcementId: z.uuid() }).strict();
const listQuery = z.object({
  limit: z.coerce.number().int().min(1).max(100).default(50),
  siteId: z.uuid().optional(),
}).strict();

const adapterSelection = z.enum([...SITE_ADAPTER_NAMES, 'auto']);

const createSiteBody = z.object({
  name: z.string().trim().min(1).max(80),
  baseUrl: z.url().max(2048),
  adapter: adapterSelection.default('new-api'),
  userId: z.string().trim().max(128).default(''),
  accessToken: z.string().min(1).max(4096),
  enabled: z.boolean().default(true),
  checkinEnabled: z.boolean().optional(),
  announcementEnabled: z.boolean().optional(),
  checkinCron: z.string().trim().min(1).max(100).default('15 8 * * *'),
  announcementCron: z.string().trim().min(1).max(100).default('*/30 * * * *'),
  timezone: z.string().trim().min(1).max(100).default('Asia/Shanghai'),
}).strict();

const patchSiteBody = z.object({
  name: z.string().trim().min(1).max(80).optional(),
  baseUrl: z.url().max(2048).optional(),
  adapter: adapterSelection.optional(),
  userId: z.string().trim().max(128).optional(),
  accessToken: z.string().min(1).max(4096).optional(),
  enabled: z.boolean().optional(),
  checkinEnabled: z.boolean().optional(),
  announcementEnabled: z.boolean().optional(),
  checkinCron: z.string().trim().min(1).max(100).optional(),
  announcementCron: z.string().trim().min(1).max(100).optional(),
  timezone: z.string().trim().min(1).max(100).optional(),
}).strict().refine((value) => Object.keys(value).length > 0, 'At least one field must be supplied');

const patchAnnouncementBody = z.object({ read: z.boolean() }).strict();

function parse<T>(schema: ZodType<T>, value: unknown): T {
  const result = schema.safeParse(value);
  if (!result.success) {
    const summary = result.error.issues.map((issue) => `${issue.path.join('.') || 'request'}: ${issue.message}`).join('; ');
    throw new AppError(422, 'VALIDATION_ERROR', summary);
  }
  return result.data;
}

function validateSchedule(cronExpression: string, timezone: string): void {
  if (!cron.validate(cronExpression)) {
    throw new AppError(422, 'VALIDATION_ERROR', 'Invalid cron expression');
  }
  try {
    assertValidTimezone(timezone);
  } catch {
    throw new AppError(422, 'VALIDATION_ERROR', 'Invalid IANA timezone');
  }
}

async function resolveAdapterSelection(
  selection: SiteAdapterSelection,
  baseUrl: string,
  dependencies: RouteDependencies,
): Promise<SiteAdapterName> {
  return selection === 'auto' ? dependencies.detector.detect(baseUrl) : selection;
}

function assertAdapterConfiguration(input: {
  adapter: SiteAdapterName;
  userId: string;
  checkinEnabled: boolean;
  announcementEnabled: boolean;
}, dependencies: RouteDependencies): void {
  const descriptor = dependencies.adapters.describe(input.adapter);
  if (descriptor.capabilities.requiresUserId && !input.userId.trim()) {
    throw new AppError(422, 'VALIDATION_ERROR', `${descriptor.displayName} requires userId`);
  }
  if (input.checkinEnabled && !descriptor.capabilities.checkin) {
    throw new AppError(422, 'VALIDATION_ERROR', `${descriptor.displayName} does not support server-side check-in`);
  }
  if (input.announcementEnabled && !descriptor.capabilities.announcements) {
    throw new AppError(422, 'VALIDATION_ERROR', `${descriptor.displayName} does not support announcement sync`);
  }
}

function serializeSite(site: SiteRecord, dependencies: RouteDependencies) {
  return toPublicSite(site, dependencies.adapters.describe(site.adapter).capabilities);
}

async function normalizeSiteChanges(
  input: z.infer<typeof patchSiteBody>,
  current: SiteRecord,
  dependencies: RouteDependencies,
): Promise<Partial<Omit<SiteRecord, 'id' | 'createdAt' | 'updatedAt' | 'consecutiveFailures'>>> {
  const checkinCron = input.checkinCron ?? current.checkinCron;
  const announcementCron = input.announcementCron ?? current.announcementCron;
  const timezone = input.timezone ?? current.timezone;
  validateSchedule(checkinCron, timezone);
  validateSchedule(announcementCron, timezone);

  const baseUrl = input.baseUrl === undefined
    ? current.baseUrl
    : normalizeBaseUrl(
      input.baseUrl,
      dependencies.config.allowInsecureHttp,
      dependencies.config.allowPrivateSites,
    );
  const adapter = input.adapter === undefined
    ? current.adapter
    : await resolveAdapterSelection(input.adapter, baseUrl, dependencies);
  const userId = input.userId ?? current.userId;
  const checkinEnabled = input.checkinEnabled ?? current.checkinEnabled;
  const announcementEnabled = input.announcementEnabled ?? current.announcementEnabled;
  assertAdapterConfiguration({ adapter, userId, checkinEnabled, announcementEnabled }, dependencies);

  const changes: Partial<Omit<SiteRecord, 'id' | 'createdAt' | 'updatedAt' | 'consecutiveFailures'>> = {};
  if (input.name !== undefined) changes.name = input.name;
  if (input.baseUrl !== undefined) {
    changes.baseUrl = baseUrl;
  }
  if (input.adapter !== undefined) changes.adapter = adapter;
  if (input.userId !== undefined) changes.userId = input.userId;
  if (input.accessToken !== undefined) changes.accessTokenCiphertext = dependencies.vault.encrypt(input.accessToken);
  if (input.enabled !== undefined) changes.enabled = input.enabled;
  if (input.checkinEnabled !== undefined) changes.checkinEnabled = input.checkinEnabled;
  if (input.announcementEnabled !== undefined) changes.announcementEnabled = input.announcementEnabled;
  if (input.checkinCron !== undefined) changes.checkinCron = input.checkinCron;
  if (input.announcementCron !== undefined) changes.announcementCron = input.announcementCron;
  if (input.timezone !== undefined) changes.timezone = input.timezone;
  return changes;
}

export async function registerApiRoutes(app: FastifyInstance, dependencies: RouteDependencies): Promise<void> {
  app.get('/api/v1/summary', async () => dependencies.db.getSummary());

  app.get('/api/v1/site-adapters', async () => ({ data: dependencies.adapters.list() }));

  app.get('/api/v1/sites', async () => {
    const sites = await dependencies.db.listSites();
    return { data: sites.map((site) => serializeSite(site, dependencies)) };
  });

  app.get('/api/v1/sites/:siteId', async (request) => {
    const { siteId } = parse(siteIdParams, request.params);
    return { data: serializeSite(await dependencies.db.getSiteOrThrow(siteId), dependencies) };
  });

  app.post('/api/v1/sites', async (request, reply) => {
    const input = parse(createSiteBody, request.body);
    validateSchedule(input.checkinCron, input.timezone);
    validateSchedule(input.announcementCron, input.timezone);
    const baseUrl = normalizeBaseUrl(
      input.baseUrl,
      dependencies.config.allowInsecureHttp,
      dependencies.config.allowPrivateSites,
    );
    const adapter = await resolveAdapterSelection(input.adapter, baseUrl, dependencies);
    const capabilities = dependencies.adapters.describe(adapter).capabilities;
    const checkinEnabled = input.checkinEnabled ?? capabilities.checkin;
    const announcementEnabled = input.announcementEnabled ?? capabilities.announcements;
    assertAdapterConfiguration({
      adapter,
      userId: input.userId,
      checkinEnabled,
      announcementEnabled,
    }, dependencies);
    const site = await dependencies.db.createSite({
      name: input.name,
      baseUrl,
      adapter,
      userId: input.userId,
      accessTokenCiphertext: dependencies.vault.encrypt(input.accessToken),
      enabled: input.enabled,
      checkinEnabled,
      announcementEnabled,
      checkinCron: input.checkinCron,
      announcementCron: input.announcementCron,
      timezone: input.timezone,
    });
    await dependencies.scheduler.reload();
    reply.header('Location', `/api/v1/sites/${site.id}`);
    return reply.code(201).send({ data: serializeSite(site, dependencies) });
  });

  app.patch('/api/v1/sites/:siteId', async (request) => {
    const { siteId } = parse(siteIdParams, request.params);
    const input = parse(patchSiteBody, request.body);
    const current = await dependencies.db.getSiteOrThrow(siteId);
    const site = await dependencies.db.updateSite(
      siteId,
      await normalizeSiteChanges(input, current, dependencies),
    );
    await dependencies.scheduler.reload();
    return { data: serializeSite(site, dependencies) };
  });

  app.post('/api/v1/sites/:siteId/checkin-runs', {
    config: { rateLimit: { max: 10, timeWindow: '1 minute' } },
  }, async (request, reply) => {
    const { siteId } = parse(siteIdParams, request.params);
    const run = await dependencies.checkins.run(siteId, request.id);
    return reply.code(run.requestId === request.id ? 201 : 200).send({ data: run });
  });

  app.get('/api/v1/checkin-runs', async (request) => {
    const query = parse(listQuery, request.query);
    return {
      data: await dependencies.db.listCheckins({
        limit: query.limit,
        ...(query.siteId ? { siteId: query.siteId } : {}),
      }),
    };
  });

  app.post('/api/v1/sites/:siteId/announcement-syncs', {
    config: { rateLimit: { max: 20, timeWindow: '1 minute' } },
  }, async (request, reply) => {
    const { siteId } = parse(siteIdParams, request.params);
    const run = await dependencies.announcements.sync(siteId, request.id);
    return reply.code(201).send({ data: run });
  });

  app.get('/api/v1/announcements', async (request) => {
    const query = parse(listQuery, request.query);
    return {
      data: await dependencies.db.listAnnouncements({
        limit: query.limit,
        ...(query.siteId ? { siteId: query.siteId } : {}),
      }),
    };
  });

  app.patch('/api/v1/announcements/:announcementId', async (request) => {
    const { announcementId } = parse(announcementIdParams, request.params);
    const input = parse(patchAnnouncementBody, request.body);
    return { data: await dependencies.db.setAnnouncementRead(announcementId, input.read) };
  });
}
