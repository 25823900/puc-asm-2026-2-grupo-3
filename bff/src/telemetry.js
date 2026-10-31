import { register } from 'node:module';

import { ExpressInstrumentation } from '@opentelemetry/instrumentation-express';
import { HttpInstrumentation } from '@opentelemetry/instrumentation-http';
import { UndiciInstrumentation } from '@opentelemetry/instrumentation-undici';
import { NodeSDK } from '@opentelemetry/sdk-node';

// Carregado com `node --import ./src/telemetry.js` para instrumentar antes do express.
// Sem OTEL_EXPORTER_OTLP_ENDPOINT nada é exportado e o serviço roda como antes.
if (process.env.OTEL_EXPORTER_OTLP_ENDPOINT) {
  register('@opentelemetry/instrumentation/hook.mjs', import.meta.url);

  const sdk = new NodeSDK({
    instrumentations: [new HttpInstrumentation(), new ExpressInstrumentation(), new UndiciInstrumentation()],
  });
  sdk.start();

  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.once(signal, () => {
      sdk.shutdown().catch(() => {});
    });
  }
}
