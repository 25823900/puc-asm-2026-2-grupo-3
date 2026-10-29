import { randomUUID } from 'node:crypto';
import express from 'express';

import { createAuth } from './auth.js';
import { createSlug } from './slug.js';

const SERVICE_NAME = 'link-service';
const SLUG_PATTERN = /^[0-9a-zA-Z]{7}$/;

export function createApp({
  db,
  cache,
  publisher = { publish: async () => {} },
  auth = createAuth(),
} = {}) {
  const app = express();

  app.get('/health', (_req, res) => {
    res.json({ status: 'ok', service: SERVICE_NAME });
  });

  app.use('/links', auth);

  app.post('/links', express.json({ limit: '10kb' }), async (req, res, next) => {
    const targetUrl = validateTargetUrl(req.body?.targetUrl);
    if (!targetUrl) {
      return res.status(400).json({
        error: { code: 'VALIDATION_ERROR', message: 'targetUrl deve ser uma URL http ou https válida' },
      });
    }

    try {
      const link = await createLink(db, req.user.id, targetUrl);
      return res.status(201).json(link);
    } catch (error) {
      return next(error);
    }
  });

  app.get('/links', async (req, res, next) => {
    try {
      return res.json(await db.listByOwner(req.user.id));
    } catch (error) {
      return next(error);
    }
  });

  app.get('/links/:id', async (req, res, next) => {
    if (!isUuid(req.params.id)) return notFound(res);
    try {
      const link = await db.findByIdForOwner(req.params.id, req.user.id);
      if (!link) return notFound(res);
      return res.json(link);
    } catch (error) {
      return next(error);
    }
  });

  app.delete('/links/:id', async (req, res, next) => {
    if (!isUuid(req.params.id)) return notFound(res);
    try {
      const link = await db.deleteByIdForOwner(req.params.id, req.user.id);
      if (!link) return notFound(res);
      await cache.del(`links:slug:${link.slug}`);
      return res.status(204).end();
    } catch (error) {
      return next(error);
    }
  });

  app.get('/resolve/:slug', async (req, res, next) => {
    const { slug } = req.params;
    if (!SLUG_PATTERN.test(slug)) return notFound(res);

    try {
      const key = `links:slug:${slug}`;
      const cached = await cache.get(key);
      const link = cached ? JSON.parse(cached) : await db.findBySlug(slug);

      if (!link) return notFound(res);
      res.set('X-Cache', cached ? 'HIT' : 'MISS');
      if (!cached) await cache.setEx(key, 3600, JSON.stringify(link));

      try {
        await publisher.publish({
          eventId: randomUUID(),
          linkId: link.id,
          slug: link.slug,
          clickedAt: new Date().toISOString(),
        });
      } catch (error) {
        console.error('Failed to publish LinkClicked event', error);
      }

      return res.json({ id: link.id, slug: link.slug, targetUrl: link.targetUrl });
    } catch (error) {
      return next(error);
    }
  });

  app.use((error, _req, res, next) => {
    if (res.headersSent) return next(error);
    if (error.type === 'entity.parse.failed') {
      return res.status(400).json({
        error: { code: 'VALIDATION_ERROR', message: 'JSON inválido' },
      });
    }
    console.error('link-service request failed', error);
    return res.status(500).json({
      error: { code: 'INTERNAL_ERROR', message: 'Falha interna' },
    });
  });

  return app;
}

async function createLink(db, ownerId, targetUrl) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const link = await db.insert({ id: randomUUID(), slug: createSlug(), ownerId, targetUrl });
    if (link) return link;
  }
  throw new Error('Could not generate a unique link slug after 5 attempts');
}

function validateTargetUrl(value) {
  if (typeof value !== 'string' || value.length === 0 || value.length > 2048) return null;
  try {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol) ? url.href : null;
  } catch {
    return null;
  }
}

function isUuid(value) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function notFound(res) {
  return res.status(404).json({
    error: { code: 'LINK_NOT_FOUND', message: 'Link não encontrado' },
  });
}
