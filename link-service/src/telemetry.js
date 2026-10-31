import { register } from 'node:module';

import { ExpressInstrumentation } from '@opentelemetry/instrumentation-express';
import { HttpInstrumentation } from '@opentelemetry/instrumentation-http';
import { KafkaJsInstrumentation } from '@opentelemetry/instrumentation-kafkajs';
import { PgInstrumentation } from '@opentelemetry/instrumentation-pg';
import { RedisInstrumentation } from '@opentelemetry/instrumentation-redis';
import { NodeSDK } from '@opentelemetry/sdk-node';

// Carregado com `node --import ./src/telemetry.js` para instrumentar antes do express.
// Sem OTEL_EXPORTER_OTLP_ENDPOINT nada é exportado e o serviço roda como antes.
if (process.env.OTEL_EXPORTER_OTLP_ENDPOINT) {
  register('@opentelemetry/instrumentation/hook.mjs', import.meta.url);

  const sdk = new NodeSDK({
    instrumentations: [
      new HttpInstrumentation(),
      new ExpressInstrumentation(),
      new PgInstrumentation(),
      new RedisInstrumentation(),
      new KafkaJsInstrumentation(),
    ],
  });
  sdk.start();

  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.once(signal, () => {
      sdk.shutdown().catch(() => {});
    });
  }
}
