import assert from 'node:assert/strict';
import test from 'node:test';

import { createLogger } from '../src/logger.js';

function capture(fn) {
  const lines = [];
  const original = process.stdout.write;
  process.stdout.write = (chunk) => {
    lines.push(String(chunk));
    return true;
  };
  try {
    fn();
  } finally {
    process.stdout.write = original;
  }
  return lines.map((line) => JSON.parse(line));
}

const spanContext = { traceId: '0af7651916cd43dd8448eb211c80319c', spanId: 'b7ad6b7169203331', traceFlags: 1 };

test('logger writes JSON lines with trace_id and span_id', () => {
  const logger = createLogger('bff', { level: 'info' });
  const [record] = capture(() => logger.info('hello', { port: 3000 }, spanContext));

  assert.equal(record.level, 'info');
  assert.equal(record.service, 'bff');
  assert.equal(record.message, 'hello');
  assert.equal(record.port, 3000);
  assert.equal(record.trace_id, spanContext.traceId);
  assert.equal(record.span_id, spanContext.spanId);
  assert.ok(!Number.isNaN(Date.parse(record.timestamp)));
});

test('logger omits trace fields without a span and serializes errors', () => {
  const logger = createLogger('bff', { level: 'info' });
  const [record] = capture(() => logger.error('failed', { error: new TypeError('boom') }));

  assert.equal(record.trace_id, undefined);
  assert.equal(record['exception.type'], 'TypeError');
  assert.equal(record['exception.message'], 'boom');
});

test('logger respects the configured level', () => {
  const quiet = createLogger('bff', { level: 'error' });
  assert.deepEqual(capture(() => quiet.info('ignored')), []);
  assert.deepEqual(capture(() => createLogger('bff', { level: 'silent' }).error('ignored')), []);
});
