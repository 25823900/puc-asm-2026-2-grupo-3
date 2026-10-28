import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { createApp } from '../src/app.js';

async function startApp(t, options) {
  const server = createApp(options).listen(0);
  t.after(() => server.close());
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

test('GET /health returns the service status', async (t) => {
  const baseUrl = await startApp(t);

  const response = await fetch(`${baseUrl}/health`);

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { status: 'ok', service: 'bff' });
});

test('GET /r/:slug redirects to the resolved target', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async (url) => {
      assert.equal(url.href, 'http://link-service:3010/resolve/aB3xK9q');
      return Response.json({ targetUrl: 'https://example.com/page' });
    },
  });

  const response = await fetch(`${baseUrl}/r/aB3xK9q`, { redirect: 'manual' });

  assert.equal(response.status, 302);
  assert.equal(response.headers.get('location'), 'https://example.com/page');
});

test('GET /r/:slug returns 404 when the link does not exist', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async () => new Response(null, { status: 404 }),
  });

  const response = await fetch(`${baseUrl}/r/missing`);

  assert.equal(response.status, 404);
  assert.deepEqual(await response.json(), {
    error: { code: 'LINK_NOT_FOUND', message: 'Link não encontrado' },
  });
});

test('GET /r/:slug returns 503 when the link-service is unavailable', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async () => {
      throw new Error('connection refused');
    },
  });

  const response = await fetch(`${baseUrl}/r/aB3xK9q`);

  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), {
    error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
  });
});

test('GET /r/:slug returns 503 when the link-service responds with an error', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async () => new Response(null, { status: 500 }),
  });

  const response = await fetch(`${baseUrl}/r/aB3xK9q`);

  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), {
    error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
  });
});

test('GET /r/:slug returns 503 when the resolved target uses an unsupported protocol', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async () => Response.json({ targetUrl: 'javascript:alert(1)' }),
  });

  const response = await fetch(`${baseUrl}/r/aB3xK9q`, { redirect: 'manual' });

  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), {
    error: { code: 'UPSTREAM_UNAVAILABLE', message: 'Serviço indisponível' },
  });
});
