import assert from 'node:assert/strict';
import { once } from 'node:events';
import { test } from 'node:test';

import { createApp } from '../src/app.js';

test('GET /health returns the service status', async (t) => {
  const server = createApp().listen(0);
  t.after(() => server.close());
  await once(server, 'listening');

  const response = await fetch(`http://127.0.0.1:${server.address().port}/health`);

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { status: 'ok', service: 'bff' });
});
