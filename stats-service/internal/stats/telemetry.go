package stats

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type logHandler struct{ console, otlp slog.Handler }

func (h logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level)
}
func (h logHandler) Handle(ctx context.Context, record slog.Record) error {
	local := record.Clone()
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		local.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
	}
	var a, b error
	if h.console.Enabled(ctx, record.Level) {
		a = h.console.Handle(ctx, local)
	}
	if h.otlp.Enabled(ctx, record.Level) {
		b = h.otlp.Handle(ctx, record)
	}
	return errors.Join(a, b)
}
func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{h.console.WithAttrs(attrs), h.otlp.WithAttrs(attrs)}
}
func (h logHandler) WithGroup(name string) slog.Handler {
	return logHandler{h.console.WithGroup(name), h.otlp.WithGroup(name)}
}

func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(env("LOG_LEVEL", "INFO")))); err != nil {
		return nil, err
	}
	console := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(console))
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	res, err := resource.New(ctx, resource.WithFromEnv(), resource.WithTelemetrySDK())
	if err != nil {
		return nil, err
	}
	var stops []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var result error
		for _, stop := range stops {
			result = errors.Join(result, stop(ctx))
		}
		return result
	}
	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithTimeout(2*time.Second))
	if err != nil {
		return shutdown, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExporter))
	stops = append(stops, tp.Shutdown)
	otel.SetTracerProvider(tp)
	metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithTimeout(2*time.Second))
	if err != nil {
		return shutdown, err
	}
	mp := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(10*time.Second))))
	stops = append(stops, mp.Shutdown)
	otel.SetMeterProvider(mp)
	logExporter, err := otlploghttp.New(ctx, otlploghttp.WithTimeout(2*time.Second))
	if err != nil {
		return shutdown, err
	}
	lp := log.NewLoggerProvider(log.WithResource(res), log.WithProcessor(log.NewBatchProcessor(logExporter)))
	stops = append(stops, lp.Shutdown)
	slog.SetDefault(slog.New(logHandler{console, otelslog.NewHandler("stats-service", otelslog.WithLoggerProvider(lp))}))
	return shutdown, nil
}
