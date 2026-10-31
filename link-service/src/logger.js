import { logs, SeverityNumber, trace } from '@opentelemetry/api';

const LEVELS = {
  debug: { rank: 10, severity: SeverityNumber.DEBUG },
  info: { rank: 20, severity: SeverityNumber.INFO },
  warn: { rank: 30, severity: SeverityNumber.WARN },
  error: { rank: 40, severity: SeverityNumber.ERROR },
};

// Escreve uma linha JSON no stdout, com trace_id e span_id do span ativo, e
// envia o mesmo registro por OTLP quando o SDK está configurado.
export function createLogger(service, { level = process.env.LOG_LEVEL ?? 'info' } = {}) {
  const minimum = level === 'silent' ? Infinity : (LEVELS[level] ?? LEVELS.info).rank;

  function log(name, message, fields = {}, spanContext = trace.getActiveSpan()?.spanContext()) {
    if (LEVELS[name].rank < minimum) return;

    const { error, ...attributes } = fields;
    if (error) {
      attributes['exception.type'] = error.name ?? 'Error';
      attributes['exception.message'] = error.message ?? String(error);
      if (error.stack) attributes['exception.stacktrace'] = error.stack;
    }

    const record = { timestamp: new Date().toISOString(), level: name, service, message, ...attributes };
    if (spanContext) {
      record.trace_id = spanContext.traceId;
      record.span_id = spanContext.spanId;
    }
    process.stdout.write(`${JSON.stringify(record)}\n`);

    logs.getLogger(service).emit({
      severityNumber: LEVELS[name].severity,
      severityText: name.toUpperCase(),
      body: message,
      attributes,
    });
  }

  return {
    debug: (message, fields, spanContext) => log('debug', message, fields, spanContext),
    info: (message, fields, spanContext) => log('info', message, fields, spanContext),
    warn: (message, fields, spanContext) => log('warn', message, fields, spanContext),
    error: (message, fields, spanContext) => log('error', message, fields, spanContext),
  };
}

export function requestLogger(logger) {
  return (req, res, next) => {
    const path = req.originalUrl.split('?')[0];
    if (path === '/health') return next();

    const startedAt = process.hrtime.bigint();
    const spanContext = trace.getActiveSpan()?.spanContext();
    res.on('finish', () => {
      logger.info(
        'request completed',
        {
          'http.request.method': req.method,
          'url.path': path,
          'http.response.status_code': res.statusCode,
          'duration_ms': Number(process.hrtime.bigint() - startedAt) / 1e6,
        },
        spanContext,
      );
    });
    next();
  };
}
