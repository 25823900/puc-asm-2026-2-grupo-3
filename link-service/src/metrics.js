import { metrics } from '@opentelemetry/api';

// Sem o SDK configurado (testes, execução local) os contadores são no-op.
const meter = metrics.getMeter('link-service');

const cacheRequests = meter.createCounter('encurta.cache.requests', {
  description: 'Consultas ao cache por resultado',
  unit: '{request}',
});

const linkClicks = meter.createCounter('encurta.link.clicks', {
  description: 'Cliques resolvidos pelo link-service, por resultado da publicação do LinkClicked',
  unit: '{click}',
});

export function recordCacheRequest(result) {
  cacheRequests.add(1, { cache: 'link-slug', result });
}

export function recordClick(outcome) {
  linkClicks.add(1, { outcome });
}
