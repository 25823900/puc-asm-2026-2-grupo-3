import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { generateKeyPair, SignJWT } from 'jose';

import { createApp } from '../src/app.js';
import { createAuth } from '../src/auth.js';

const ISSUER = 'http://localhost:8080/realms/encurta';
const OWNER_ID = 'f3c1d2e4-0000-4000-8000-000000000001';
const { publicKey, privateKey } = await generateKeyPair('RS256');

async function sign(issuer = ISSUER) {
  return new SignJWT({})
    .setProtectedHeader({ alg: 'RS256' })
    .setIssuer(issuer)
    .setSubject(OWNER_ID)
    .setExpirationTime('5m')
    .sign(privateKey);
}

async function startApp(t) {
  const server = createApp({
    auth: createAuth({ issuer: ISSUER, jwks: publicKey }),
    db: { listByOwner: async (ownerId) => [{ ownerId }] },
    cache: {},
  }).listen(0);
  t.after(() => server.close());
  await once(server, 'listening');
  return `http://127.0.0.1:${server.address().port}`;
}

test('private link routes reject requests without a token', async (t) => {
  const baseUrl = await startApp(t);

  const response = await fetch(`${baseUrl}/links`);

  assert.equal(response.status, 401);
  assert.equal((await response.json()).error.code, 'UNAUTHORIZED');
});

test('private link routes reject tokens with an invalid issuer', async (t) => {
  const baseUrl = await startApp(t);
  const token = await sign('http://invalid.example/realms/encurta');

  const response = await fetch(`${baseUrl}/links`, {
    headers: { authorization: `Bearer ${token}` },
  });

  assert.equal(response.status, 401);
});

test('private link routes use the subject from a valid token as owner', async (t) => {
  const baseUrl = await startApp(t);
  const token = await sign();

  const response = await fetch(`${baseUrl}/links`, {
    headers: { authorization: `Bearer ${token}` },
  });

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), [{ ownerId: OWNER_ID }]);
});
