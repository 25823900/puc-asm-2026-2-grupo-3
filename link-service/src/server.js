import { createApp } from './app.js';
import { createDependencies } from './clients.js';
import { createLogger } from './logger.js';

const port = Number(process.env.PORT ?? 3010);
const logger = createLogger('link-service');

const dependencies = await createDependencies();
const server = createApp(dependencies).listen(port, () => {
  logger.info('link-service listening', { port });
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    server.close(async () => {
      await dependencies.close();
      process.exit(0);
    });
  });
}
