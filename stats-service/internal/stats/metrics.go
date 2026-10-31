package stats

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// O meter global delega ao provider configurado em Setup; sem OTLP os contadores são no-op.
var (
	meter           = otel.Meter("stats-service")
	cacheRequests   metric.Int64Counter
	clicksProcessed metric.Int64Counter
)

func init() {
	cacheRequests, _ = meter.Int64Counter("encurta.cache.requests",
		metric.WithDescription("Consultas ao cache por resultado"), metric.WithUnit("{request}"))
	clicksProcessed, _ = meter.Int64Counter("encurta.stats.clicks.processed",
		metric.WithDescription("Cliques LinkClicked gravados pelo stats-service"), metric.WithUnit("{click}"))
}

func recordCache(ctx context.Context, result string) {
	cacheRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("cache", "stats-summary"), attribute.String("result", result)))
}
