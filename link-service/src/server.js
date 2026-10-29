import { createApp } from './app.js';
import { createDependencies } from './clients.js';

const port = Number(process.env.PORT ?? 3010);

const dependencies = await createDependencies();
const server = createApp(dependencies).listen(port, () => {
  console.log(`link-service listening on port ${port}`);
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    server.close(async () => {
      await dependencies.close();
      process.exit(0);
    });
  });
}
