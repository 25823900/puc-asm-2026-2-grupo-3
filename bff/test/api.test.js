import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { createApp } from '../src/app.js';

const link = {
  id: '7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11',
  slug: 'aB3xK9q',
  targetUrl: 'https://example.com/page',
  ownerId: 'f3c1d2e4-0000-4000-8000-000000000001',
  createdAt: '2026-10-08T21:30:00.000Z',
};

async function startApp(t, fetchImpl) {
  const auth = (_req, _res, next) => next();
  const server = createApp({ auth, fetchImpl }).listen(0);
  t.after(() => server.close());
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

test('POST /api/links forwards the bearer token and returns the public link shape', async (t) => {
  const baseUrl = await startApp(t, async (url, options) => {
    assert.equal(url.href, 'http://link-service:3010/links');
    assert.equal(options.method, 'POST');
    assert.equal(options.headers.authorization, 'Bearer test-token');
    assert.deepEqual(JSON.parse(options.body), { targetUrl: link.targetUrl });
    return Response.json(link, { status: 201 });
  });

  const response = await fetch(`${baseUrl}/api/links`, {
    method: 'POST',
    headers: {
      authorization: 'Bearer test-token',
      'content-type': 'application/json',
    },
    body: JSON.stringify({ targetUrl: link.targetUrl }),
  });

  assert.equal(response.status, 201);
  assert.deepEqual(await response.json(), {
    id: link.id,
    slug: link.slug,
    shortUrl: `http://localhost:3000/r/${link.slug}`,
    targetUrl: link.targetUrl,
    createdAt: link.createdAt,
  });
});

test('GET /api/links combines owned links with click totals', async (t) => {
  let requests = 0;
  const baseUrl = await startApp(t, async (url, options) => {
    requests += 1;
    if (requests === 1) {
      assert.equal(url.href, 'http://link-service:3010/links');
      assert.equal(options.headers.authorization, 'Bearer test-token');
      return Response.json([link]);
    }

    assert.equal(url.href, `http://stats-service:8000/stats?linkIds=${link.id}`);
    return Response.json([{ linkId: link.id, totalClicks: 42 }]);
  });

  const response = await fetch(`${baseUrl}/api/links`, {
    headers: { authorization: 'Bearer test-token' },
  });

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), [
    {
      id: link.id,
      slug: link.slug,
      shortUrl: `http://localhost:3000/r/${link.slug}`,
      targetUrl: link.targetUrl,
      createdAt: link.createdAt,
      totalClicks: 42,
    },
  ]);
});

test('GET /api/links/:id/stats confirms ownership before returning stats', async (t) => {
  let requests = 0;
  const stats = {
    linkId: link.id,
    totalClicks: 1,
    clicksPerDay: [{ date: '2026-10-08', clicks: 1 }],
  };
  const baseUrl = await startApp(t, async (url, options) => {
    requests += 1;
    if (requests === 1) {
      assert.equal(url.href, `http://link-service:3010/links/${link.id}`);
      assert.equal(options.headers.authorization, 'Bearer test-token');
      return Response.json(link);
    }

    assert.equal(url.href, `http://stats-service:8000/stats/${link.id}`);
    return Response.json(stats, { headers: { 'X-Cache': 'MISS' } });
  });

  const response = await fetch(`${baseUrl}/api/links/${link.id}/stats`, {
    headers: { authorization: 'Bearer test-token' },
  });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('x-cache'), 'MISS');
  assert.deepEqual(await response.json(), stats);
});

test('DELETE /api/links/:id forwards the request and returns 204', async (t) => {
  const baseUrl = await startApp(t, async (url, options) => {
    assert.equal(url.href, `http://link-service:3010/links/${link.id}`);
    assert.equal(options.method, 'DELETE');
    assert.equal(options.headers.authorization, 'Bearer test-token');
    return new Response(null, { status: 204 });
  });

  const response = await fetch(`${baseUrl}/api/links/${link.id}`, {
    method: 'DELETE',
    headers: { authorization: 'Bearer test-token' },
  });

  assert.equal(response.status, 204);
});

test('GET /api/links returns null totals when stats-service is unavailable', async (t) => {
  let requests = 0;
  const baseUrl = await startApp(t, async () => {
    requests += 1;
    if (requests === 1) return Response.json([link]);
    throw new Error('connection refused');
  });

  const response = await fetch(`${baseUrl}/api/links`, {
    headers: { authorization: 'Bearer test-token' },
  });

  assert.equal(response.status, 200);
  assert.equal((await response.json())[0].totalClicks, null);
});
