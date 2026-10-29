import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { generateKeyPair, SignJWT } from 'jose';

import { createApp } from '../src/app.js';
import { createAuth } from '../src/auth.js';

const ISSUER = 'http://localhost:8080/realms/encurta';
const ALICE = 'f3c1d2e4-0000-4000-8000-000000000001';

const { publicKey, privateKey } = await generateKeyPair('RS256');

function sign({ key = privateKey, issuer = ISSUER, subject = ALICE, expiresIn = '5m' } = {}) {
  const jwt = new SignJWT({ ...(subject && { sub: subject }) })
    .setProtectedHeader({ alg: 'RS256' })
    .setIssuer(issuer)
    .setExpirationTime(expiresIn);
  return jwt.sign(key);
}

async function startApp(t, options = {}) {
  const auth = createAuth({ issuer: ISSUER, jwks: publicKey });
  const server = createApp({ ...options, auth }).listen(0);
  t.after(() => server.close());
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

const getApi = (baseUrl, authorization) =>
  fetch(`${baseUrl}/api/links`, { headers: authorization ? { authorization } : {} });

test('/api without a token returns 401 in the error format', async (t) => {
  const baseUrl = await startApp(t);

  const response = await getApi(baseUrl);

  assert.equal(response.status, 401);
  assert.deepEqual(await response.json(), {
    error: { code: 'UNAUTHORIZED', message: 'Token ausente, inválido ou expirado' },
  });
});

test('/api rejects invalid tokens with 401', async (t) => {
  const baseUrl = await startApp(t);
  const { privateKey: otherKey } = await generateKeyPair('RS256');
  const cases = {
    malformed: 'Bearer not-a-jwt',
    expired: `Bearer ${await sign({ expiresIn: '-1m' })}`,
    'wrong issuer': `Bearer ${await sign({ issuer: 'http://evil.example/realms/encurta' })}`,
    'wrong signature': `Bearer ${await sign({ key: otherKey })}`,
    'missing sub': `Bearer ${await sign({ subject: null })}`,
    'not a Bearer scheme': `Basic ${await sign()}`,
  };

  for (const [name, authorization] of Object.entries(cases)) {
    assert.equal((await getApi(baseUrl, authorization)).status, 401, name);
  }
});

test('/api accepts a valid token and goes on to the route', async (t) => {
  const baseUrl = await startApp(t, {
    fetchImpl: async () => Response.json([]),
  });

  const response = await getApi(baseUrl, `Bearer ${await sign()}`);

  assert.equal(response.status, 200);
});

test('requireAuth exposes the sub claim as req.user.id', async () => {
  const requireAuth = createAuth({ issuer: ISSUER, jwks: publicKey });
  const token = await sign();
  const req = { get: (name) => (name === 'authorization' ? `Bearer ${token}` : undefined) };
  let called = false;

  await requireAuth(req, {}, () => {
    called = true;
  });

  assert.equal(called, true);
  assert.deepEqual(req.user, { id: ALICE });
});

test('/health and /r/:slug do not require a token', async (t) => {
  const baseUrl = await startApp(t);

  assert.equal((await fetch(`${baseUrl}/health`)).status, 200);
  // Sem link-service disponível o redirecionamento falha com 503, nunca com 401.
  assert.notEqual((await fetch(`${baseUrl}/r/aB3xK9q`, { redirect: 'manual' })).status, 401);
});
