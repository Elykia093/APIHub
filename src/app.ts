import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import Fastify, { type FastifyInstance } from 'fastify';
import fastifyStatic from '@fastify/static';
import helmet from '@fastify/helmet';
import rateLimit from '@fastify/rate-limit';
import type { AppConfig } from './config.js';
import { AppDatabase } from './db/database.js';
import { AppError, asAppError } from './lib/errors.js';
import { CredentialVault, tokensEqual } from './security/crypto.js';
import { SafeHttpClient } from './security/safe-http.js';
import { NewApiAdapter } from './adapters/new-api.js';
import { Sub2ApiAdapter } from './adapters/sub2api.js';
import { ZenApiAdapter } from './adapters/zen-api.js';
import { SiteAdapterDetector } from './adapters/detector.js';
import { AdapterRegistry } from './adapters/registry.js';
import { CheckinService } from './services/checkin-service.js';
import { AnnouncementService } from './services/announcement-service.js';
import { SchedulerService } from './services/scheduler.js';
import { registerApiRoutes } from './routes/api.js';

export type AppRuntime = {
  app: FastifyInstance;
  db: AppDatabase;
  http: SafeHttpClient;
  scheduler: SchedulerService;
};

export async function buildApp(
  config: AppConfig,
  options: { startScheduler?: boolean; database?: AppDatabase } = {},
): Promise<AppRuntime> {
  const app = Fastify({
    logger: {
      level: config.nodeEnv === 'test' ? 'silent' : 'info',
      redact: {
        paths: [
          'req.headers.authorization',
          'request.headers.authorization',
          '*.accessToken',
          '*.access_token',
          '*.cookie',
        ],
        censor: '[REDACTED]',
      },
    },
    bodyLimit: 64 * 1024,
    requestIdHeader: false,
    trustProxy: false,
  });

  await app.register(helmet, {
    contentSecurityPolicy: {
      directives: {
        defaultSrc: ["'self'"],
        scriptSrc: ["'self'"],
        styleSrc: ["'self'"],
        imgSrc: ["'self'", 'data:'],
        connectSrc: ["'self'"],
        objectSrc: ["'none'"],
        baseUri: ["'self'"],
        frameAncestors: ["'none'"],
      },
    },
  });
  await app.register(rateLimit, {
    global: true,
    max: 240,
    timeWindow: '1 minute',
  });

  const db = options.database ?? await AppDatabase.connect({
    connectionString: config.databaseUrl,
    max: config.databasePoolMax,
    idleTimeoutMs: config.databaseIdleTimeoutMs,
    connectionTimeoutMs: config.databaseConnectionTimeoutMs,
    statementTimeoutMs: config.databaseStatementTimeoutMs,
    onPoolError: (error) => app.log.error({ err: error }, 'idle PostgreSQL client error'),
  });
  const vault = new CredentialVault(config.appSecret);
  const http = new SafeHttpClient({
    timeoutMs: config.httpTimeoutMs,
    maxResponseBytes: config.maxResponseBytes,
    allowPrivateSites: config.allowPrivateSites,
    allowInsecureHttp: config.allowInsecureHttp,
  });
  const adapters = new AdapterRegistry([
    new NewApiAdapter(http),
    new Sub2ApiAdapter(http),
    new ZenApiAdapter(http),
  ]);
  const detector = new SiteAdapterDetector(http);
  const checkins = new CheckinService(db, adapters, vault, app.log);
  const announcements = new AnnouncementService(db, adapters, vault, app.log);
  const scheduler = new SchedulerService(db, checkins, announcements, app.log);

  app.setErrorHandler(async (error, request, reply) => {
    const appError = asAppError(error);
    if (appError.statusCode >= 500) {
      request.log.error({ err: error, requestId: request.id, errorCode: appError.code }, 'request failed');
    }
    return reply.code(appError.statusCode).send({
      error: {
        code: appError.code,
        message: appError.message,
        retryable: appError.retryable,
        requestId: request.id,
      },
    });
  });

  app.addHook('onRequest', async (request, reply) => {
    if (!request.url.startsWith('/api/v1/')) return;
    reply.header('Cache-Control', 'no-store');
    const authorization = typeof request.headers.authorization === 'string'
      ? request.headers.authorization
      : '';
    const match = /^Bearer\s+(.+)$/i.exec(authorization);
    if (!match?.[1] || !tokensEqual(match[1], config.adminToken)) {
      throw new AppError(401, 'AUTH_REQUIRED', 'Administrator authentication required');
    }
  });

  app.get('/health/live', async () => ({ status: 'ok' }));
  app.get('/health/ready', async () => {
    await db.assertReady();
    return { status: 'ready' };
  });

  await registerApiRoutes(app, {
    config,
    db,
    vault,
    adapters,
    detector,
    checkins,
    announcements,
    scheduler,
  });

  await app.register(fastifyStatic, {
    root: resolve(dirname(fileURLToPath(import.meta.url)), '../public'),
    prefix: '/',
    wildcard: false,
  });

  app.setNotFoundHandler(async (request, reply) => {
    if (request.url.startsWith('/api/') || request.url.startsWith('/health/')) {
      throw new AppError(404, 'NOT_FOUND', 'Route not found');
    }
    return reply.sendFile('index.html');
  });

  app.addHook('onClose', async () => {
    scheduler.stop();
    await http.close();
    await db.close();
  });

  if (options.startScheduler !== false) await scheduler.reload();
  return { app, db, http, scheduler };
}
