package stats

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type MessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
}

func eventContext(ctx context.Context, msg kafka.Message) context.Context {
	headers := propagation.MapCarrier{}
	for _, h := range msg.Headers {
		headers[strings.ToLower(h.Key)] = string(h.Value)
	}
	return otel.GetTextMapPropagator().Extract(ctx, headers)
}

func processBatch(ctx context.Context, repo Repository, cache SummaryCache, messages []kafka.Message) error {
	clicks := make([]Click, 0, len(messages))
	ids := make([]uuid.UUID, 0, len(messages))
	seen := make(map[uuid.UUID]bool, len(messages))
	spans := make([]trace.Span, 0, len(messages))
	writeCtx := ctx
	for _, msg := range messages {
		spanCtx, span := otel.Tracer("stats.consumer").Start(eventContext(ctx, msg), msg.Topic+" process",
			trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.String("messaging.operation.type", "process"),
				attribute.String("messaging.destination.name", msg.Topic),
				attribute.Int("messaging.destination.partition.id", msg.Partition),
				attribute.Int64("messaging.kafka.offset", msg.Offset)))
		spans = append(spans, span)
		click, err := parseClick(msg.Value)
		if err != nil {
			slog.WarnContext(spanCtx, "Mensagem LinkClicked inválida descartada", "error", err)
			continue
		}
		if len(clicks) == 0 {
			writeCtx = spanCtx
		}
		clicks = append(clicks, click)
		if !seen[click.LinkID] {
			seen[click.LinkID] = true
			ids = append(ids, click.LinkID)
		}
	}
	defer func() {
		for _, span := range spans {
			span.End()
		}
	}()
	if len(clicks) == 0 {
		return nil
	}
	dbCtx, cancel := context.WithTimeout(writeCtx, 5*time.Second)
	err := repo.InsertBatch(dbCtx, clicks)
	cancel()
	if err != nil {
		for _, span := range spans {
			span.RecordError(err)
			span.SetStatus(codes.Error, "falha ao gravar cliques")
		}
		return err
	}
	// Invalida também em replay: corrige crash entre o COMMIT SQL e Redis.
	if err := cache.Invalidate(writeCtx, ids); err != nil {
		slog.WarnContext(writeCtx, "Falha ao invalidar cache; TTL limita a defasagem", "error", err)
	}
	return nil
}

func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Lotes de até 128 mensagens ou 25 ms amortizam SQL, Redis e commits.
// Não lê/commita mensagens futuras enquanto um lote não foi persistido.
func consume(ctx context.Context, reader MessageReader, repo Repository, cache SummaryCache) {
	for ctx.Err() == nil {
		first, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "Falha ao ler Kafka", "error", err)
			if !waitRetry(ctx, time.Second) {
				return
			}
			continue
		}
		batch := make([]kafka.Message, 0, 128)
		batch = append(batch, first)
		batchCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		for len(batch) < cap(batch) {
			msg, err := reader.FetchMessage(batchCtx)
			if err != nil {
				break
			}
			batch = append(batch, msg)
		}
		cancel()
		backoff := 100 * time.Millisecond
		for ctx.Err() == nil {
			err = processBatch(ctx, repo, cache, batch)
			if err == nil {
				break
			}
			slog.ErrorContext(ctx, "Lote não persistido; offsets não confirmados", "error", err)
			if !waitRetry(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 5*time.Second)
		}
		if ctx.Err() != nil {
			return
		}
		backoff = 100 * time.Millisecond
		for ctx.Err() == nil {
			commitCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = reader.CommitMessages(commitCtx, batch...)
			stop()
			if err == nil {
				break
			}
			slog.WarnContext(ctx, "Falha ao confirmar lote Kafka", "error", err)
			if !waitRetry(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 5*time.Second)
		}
	}
}
