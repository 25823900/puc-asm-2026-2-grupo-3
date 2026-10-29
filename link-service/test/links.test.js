import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { createApp } from '../src/app.js';

const OWNER_ID = 'f3c1d2e4-0000-4000-8000-000000000001';
const OTHER_OWNER_ID = 'f3c1d2e4-0000-4000-8000-000000000002';
const LINK_ID = '7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11';
const link = {
  id: LINK_ID,
  slug: 'aB3xK9q',
  targetUrl: 'https://example.com/page',
  ownerId: OWNER_ID,
  createdAt: '2026-10-08T21:30:00.000Z',
};

async function startApp(t, dependencies = {}) {
  const db = {
    insert: async (created) => ({ ...created, createdAt: link.createdAt }),
    listByOwner: async () => [link],
    findByIdForOwner: async () => link,
    deleteByIdForOwner: async () => ({ slug: link.slug }),
    findBySlug: async () => link,
    ...dependencies.db,
  };
  const cache = {
    get: async () => null,
    setEx: async () => {},
    del: async () => {},
    ...dependencies.cache,
  };
  const auth = dependencies.auth ?? ((_req, _res, next) => {
    _req.user = { id: OWNER_ID };
    next();
  });
  const server = createApp({
    db,
    cache,
    auth,
    publisher: dependencies.publisher,
  }).listen(0);
  t.after(() => server.close());
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

test('POST /links validates and stores an owned link', async (t) => {
  let inserted;
  const baseUrl = await startApp(t, {
    db: {
      insert: async (data) => {
        inserted = data;
        return { ...data, createdAt: link.createdAt };
      },
    },
  });

  const response = await fetch(`${baseUrl}/links`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ targetUrl: link.targetUrl }),
  });
  const body = await response.json();

  assert.equal(response.status, 201);
  assert.equal(body.ownerId, OWNER_ID);
  assert.equal(body.targetUrl, link.targetUrl);
  assert.match(body.id, /^[0-9a-f-]{36}$/i);
  assert.match(body.slug, /^[0-9a-zA-Z]{7}$/);
  assert.equal(inserted.ownerId, OWNER_ID);
  assert.equal(inserted.targetUrl, link.targetUrl);
});

test('POST /links rejects invalid URLs and unsupported protocols', async (t) => {
  const baseUrl = await startApp(t, {
    db: { insert: async () => assert.fail('invalid URL should not be persisted') },
  });

  for (const targetUrl of ['not a URL', 'ftp://example.com', 'javascript:alert(1)', '']) {
    const response = await fetch(`${baseUrl}/links`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ targetUrl }),
    });
    assert.equal(response.status, 400, targetUrl);
    assert.equal((await response.json()).error.code, 'VALIDATION_ERROR');
  }
});

test('POST /links rejects targets longer than 2048 characters', async (t) => {
  const baseUrl = await startApp(t);
  const targetUrl = `https://example.com/${'a'.repeat(2048)}`;

  const response = await fetch(`${baseUrl}/links`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ targetUrl }),
  });

  assert.equal(response.status, 400);
});

test('GET /links lists only the authenticated owner links', async (t) => {
  let requestedOwner;
  const baseUrl = await startApp(t, {
    db: {
      listByOwner: async (ownerId) => {
        requestedOwner = ownerId;
        return [link];
      },
    },
  });

  const response = await fetch(`${baseUrl}/links`);

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), [link]);
  assert.equal(requestedOwner, OWNER_ID);
});

test('GET /links/:id treats links of other owners as not found', async (t) => {
  const baseUrl = await startApp(t, {
    db: {
      findByIdForOwner: async (_id, ownerId) => ownerId === OTHER_OWNER_ID ? link : null,
    },
  });

  const response = await fetch(`${baseUrl}/links/${LINK_ID}`);

  assert.equal(response.status, 404);
  assert.equal((await response.json()).error.code, 'LINK_NOT_FOUND');
});

test('DELETE /links/:id removes the owned link and invalidates its cache', async (t) => {
  let invalidatedKey;
  const baseUrl = await startApp(t, {
    cache: {
      del: async (key) => {
        invalidatedKey = key;
      },
    },
  });

  const response = await fetch(`${baseUrl}/links/${LINK_ID}`, { method: 'DELETE' });

  assert.equal(response.status, 204);
  assert.equal(invalidatedKey, `links:slug:${link.slug}`);
});

test('GET /resolve/:slug caches the target and publishes a click event', async (t) => {
  const publishedEvents = [];
  let cached = false;
  const baseUrl = await startApp(t, {
    cache: {
      get: async () => (cached ? JSON.stringify(link) : null),
      setEx: async (key, ttl, value) => {
        assert.equal(key, `links:slug:${link.slug}`);
        assert.equal(ttl, 3600);
        assert.deepEqual(JSON.parse(value), link);
        cached = true;
      },
    },
    publisher: {
      publish: async (event) => publishedEvents.push(event),
    },
  });

  const first = await fetch(`${baseUrl}/resolve/${link.slug}`);
  assert.equal(first.status, 200);
  assert.equal(first.headers.get('x-cache'), 'MISS');
  assert.deepEqual(await first.json(), {
    id: LINK_ID,
    slug: link.slug,
    targetUrl: link.targetUrl,
  });

  const second = await fetch(`${baseUrl}/resolve/${link.slug}`);
  assert.equal(second.headers.get('x-cache'), 'HIT');
  await second.json();

  assert.equal(publishedEvents.length, 2);
  assert.equal(publishedEvents[0].linkId, LINK_ID);
  assert.equal(publishedEvents[0].slug, link.slug);
  assert.match(publishedEvents[0].eventId, /^[0-9a-f-]{36}$/i);
  assert.ok(Number.isFinite(Date.parse(publishedEvents[0].clickedAt)));
});

test('GET /resolve/:slug returns 404 for unknown slugs', async (t) => {
  const baseUrl = await startApp(t, {
    db: { findBySlug: async () => null },
  });

  const response = await fetch(`${baseUrl}/resolve/unknown`);

  assert.equal(response.status, 404);
});

test('GET /links requires authentication', async (t) => {
  const baseUrl = await startApp(t, {
    auth: (_req, res) => res.status(401).json({
      error: { code: 'UNAUTHORIZED', message: 'Token ausente, inválido ou expirado' },
    }),
  });

  const response = await fetch(`${baseUrl}/links`);

  assert.equal(response.status, 401);
});
