import express from 'express';

const SERVICE_NAME = 'bff';

export function createApp() {
  const app = express();

  app.get('/health', (_req, res) => {
    res.json({ status: 'ok', service: SERVICE_NAME });
  });

  return app;
}
