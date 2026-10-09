import { createApp } from './app.js';

const port = Number(process.env.PORT ?? 3010);

const server = createApp().listen(port, () => {
  console.log(`link-service listening on port ${port}`);
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
