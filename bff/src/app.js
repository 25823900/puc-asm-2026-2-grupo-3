import express from 'express';

import { createAuth } from './auth.js';

const SERVICE_NAME = 'bff';

export function createApp({
  linkServiceUrl = process.env.LINK_SERVICE_URL ?? 'http://link-service:3010',
  fetchImpl = fetch,
  auth = createAuth(),
} = {}) {
  const app = express();

  app.get('/health', (_req, res) => {
    res.json({ status: 'ok', service: SERVICE_NAME });
  });

  app.get('/r/:slug', async (req, res) => {
    try {
      const url = new URL(`/resolve/${encodeURIComponent(req.params.slug)}`, linkServiceUrl);
      const upstream = await fetchImpl(url);

      if (upstream.status === 404) {
        return res.status(404).json({
          error: { code: 'LINK_NOT_FOUND', message: 'Link não encontrado' },
        });
      }

      if (!upstream.ok) {
        return res.status(503).json({
          error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
        });
      }

      const link = await upstream.json();
      const target = new URL(link.targetUrl);
      if (!['http:', 'https:'].includes(target.protocol)) {
        throw new Error('Invalid link target');
      }

      return res.redirect(302, target.href);
    } catch {
      return res.status(503).json({
        error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
      });
    }
  });

  // Tudo em /api exige token. /health e /r/:slug ficam públicos.
  // As rotas de /api/links entram depois deste middleware e podem usar req.user.id.
  app.use('/api', auth);

  return app;
}
