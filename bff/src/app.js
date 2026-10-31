import express from 'express';

import { createAuth } from './auth.js';
import { createLogger, requestLogger } from './logger.js';

const SERVICE_NAME = 'bff';

export function createApp({
  linkServiceUrl = process.env.LINK_SERVICE_URL ?? 'http://link-service:3010',
  statsServiceUrl = process.env.STATS_SERVICE_URL ?? 'http://stats-service:8000',
  publicBaseUrl = process.env.PUBLIC_BASE_URL ?? 'http://localhost:3000',
  fetchImpl = fetch,
  auth = createAuth(),
  logger = createLogger(SERVICE_NAME),
} = {}) {
  const app = express();

  app.use(requestLogger(logger));

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
    } catch (error) {
      logger.error('Failed to resolve link redirect', { error });
      return res.status(503).json({
        error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
      });
    }
  });

  // Tudo em /api exige token. /health e /r/:slug ficam públicos.
  app.use('/api', auth);
  app.use('/api', express.json({ limit: '10kb' }));

  app.post('/api/links', async (req, res) => {
    try {
      const upstream = await fetchImpl(new URL('/links', linkServiceUrl), {
        method: 'POST',
        headers: {
          authorization: req.get('authorization'),
          'content-type': 'application/json',
        },
        body: JSON.stringify(req.body),
      });
      if (!upstream.ok) return forwardError(upstream, res);

      const link = await upstream.json();
      return res.status(201).json(toPublicLink(link, publicBaseUrl));
    } catch (error) {
      logger.error('Failed to create link through link-service', { error });
      return upstreamUnavailable(res);
    }
  });

  app.get('/api/links', async (req, res) => {
    try {
      const upstream = await fetchImpl(new URL('/links', linkServiceUrl), {
        headers: { authorization: req.get('authorization') },
      });
      if (!upstream.ok) return forwardError(upstream, res);

      const links = await upstream.json();
      let totals = new Map();
      if (links.length > 0) {
        try {
          const statsUrl = new URL('/stats', statsServiceUrl);
          statsUrl.searchParams.set('linkIds', links.map(({ id }) => id).join(','));
          const statsResponse = await fetchImpl(statsUrl);
          if (statsResponse.ok) {
            const summaries = await statsResponse.json();
            totals = new Map(summaries.map(({ linkId, totalClicks }) => [linkId, totalClicks]));
          } else {
            logger.warn('stats-service returned an error for link summaries', {
              'http.response.status_code': statsResponse.status,
            });
          }
        } catch (error) {
          logger.error('Failed to retrieve click summaries from stats-service', { error });
        }
      }

      return res.json(
        links.map((link) => ({
          ...toPublicLink(link, publicBaseUrl),
          totalClicks: totals.get(link.id) ?? null,
        })),
      );
    } catch (error) {
      logger.error('Failed to list links through link-service', { error });
      return upstreamUnavailable(res);
    }
  });

  app.get('/api/links/:id/stats', async (req, res) => {
    try {
      const ownedLink = await fetchImpl(new URL(`/links/${encodeURIComponent(req.params.id)}`, linkServiceUrl), {
        headers: { authorization: req.get('authorization') },
      });
      if (!ownedLink.ok) return forwardError(ownedLink, res);

      const stats = await fetchImpl(
        new URL(`/stats/${encodeURIComponent(req.params.id)}`, statsServiceUrl),
      );
      if (!stats.ok) return upstreamUnavailable(res);
      const body = await stats.json();
      const cacheStatus = stats.headers.get('X-Cache');
      if (cacheStatus) res.set('X-Cache', cacheStatus);
      return res.json(body);
    } catch (error) {
      logger.error('Failed to retrieve link statistics', { error });
      return upstreamUnavailable(res);
    }
  });

  app.delete('/api/links/:id', async (req, res) => {
    try {
      const upstream = await fetchImpl(new URL(`/links/${encodeURIComponent(req.params.id)}`, linkServiceUrl), {
        method: 'DELETE',
        headers: { authorization: req.get('authorization') },
      });
      if (!upstream.ok) return forwardError(upstream, res);
      return res.status(204).end();
    } catch (error) {
      logger.error('Failed to delete link through link-service', { error });
      return upstreamUnavailable(res);
    }
  });

  app.use((error, _req, res, next) => {
    if (res.headersSent) return next(error);
    if (error.type === 'entity.parse.failed') {
      return res.status(400).json({
        error: { code: 'VALIDATION_ERROR', message: 'JSON inválido' },
      });
    }
    logger.error('BFF request failed', { error });
    return upstreamUnavailable(res);
  });

  return app;
}

function toPublicLink(link, publicBaseUrl) {
  return {
    id: link.id,
    slug: link.slug,
    shortUrl: new URL(`/r/${encodeURIComponent(link.slug)}`, publicBaseUrl).href,
    targetUrl: link.targetUrl,
    createdAt: link.createdAt,
  };
}

async function forwardError(upstream, res) {
  const status = upstream.status === 401 ? 401 : upstream.status === 400 ? 400 : upstream.status === 404 ? 404 : 503;
  const code =
    status === 401
      ? 'UNAUTHORIZED'
      : status === 400
        ? 'VALIDATION_ERROR'
        : status === 404
          ? 'LINK_NOT_FOUND'
          : 'UPSTREAM_UNAVAILABLE';
  return res.status(status).json({
    error: {
      code,
      message:
        code === 'UNAUTHORIZED'
          ? 'Token ausente, inválido ou expirado'
          : code === 'VALIDATION_ERROR'
            ? 'targetUrl deve ser uma URL http ou https válida'
            : code === 'LINK_NOT_FOUND'
              ? 'Link não encontrado'
              : 'Serviço indisponível',
    },
  });
}

function upstreamUnavailable(res) {
  return res.status(503).json({
    error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
  });
}
