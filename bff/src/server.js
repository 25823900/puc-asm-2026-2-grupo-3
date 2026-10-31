import { createApp } from './app.js';
import { createLogger } from './logger.js';

const port = Number(process.env.PORT ?? 3000);
const logger = createLogger('bff');

const server = createApp().listen(port, () => {
  logger.info('bff listening', { port });
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
