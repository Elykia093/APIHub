import { loadConfig } from './config.js';
import { buildApp } from './app.js';

const config = loadConfig();
const runtime = await buildApp(config);

let shuttingDown = false;
async function shutdown(signal: string): Promise<void> {
  if (shuttingDown) return;
  shuttingDown = true;
  runtime.app.log.info({ signal }, 'shutting down');
  await runtime.app.close();
}

process.once('SIGINT', () => void shutdown('SIGINT'));
process.once('SIGTERM', () => void shutdown('SIGTERM'));

try {
  await runtime.app.listen({ host: config.host, port: config.port });
  runtime.app.log.info({ host: config.host, port: config.port }, 'APIHub started');
} catch (error) {
  runtime.app.log.error({ err: error }, 'failed to start');
  await runtime.app.close();
  process.exitCode = 1;
}
