package stats

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

type API struct {
	repo    Repository
	cache   SummaryCache
	flights singleflight.Group
}

func newHandler(repo Repository, cache SummaryCache) http.Handler {
	api := &API{repo: repo, cache: cache}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "stats-service"})
	})
	mux.Handle("GET /stats", instrumentRoute("/stats", api.totals))
	mux.Handle("GET /stats/{linkId}", instrumentRoute("/stats/{linkId}", api.summary))
	return mux
}

func instrumentRoute(route string, handler http.HandlerFunc) http.Handler {
	attrs := []attribute.KeyValue{attribute.String("http.route", route)}
	return otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace.SpanFromContext(r.Context()).SetAttributes(attrs...)
		handler(w, r)
	}), "GET "+route, otelhttp.WithMetricAttributesFn(func(*http.Request) []attribute.KeyValue { return attrs }))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func parseIDs(raw string) ([]uuid.UUID, error) {
	parts := strings.Split(raw, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	seen := make(map[uuid.UUID]bool, len(parts))
	for _, part := range parts {
		id, err := uuid.Parse(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, nil
}

func (a *API) totals(w http.ResponseWriter, r *http.Request) {
	ids, err := parseIDs(r.URL.Query().Get("linkIds"))
	if err != nil {
		apiError(w, 400, "VALIDATION_ERROR", "linkIds deve ser uma lista de UUIDs separados por vírgula")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	totals, err := a.repo.Totals(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "Falha ao consultar totais", "error", err)
		apiError(w, 500, "INTERNAL_ERROR", "Erro interno")
		return
	}
	result := make([]Total, len(ids))
	for i, id := range ids {
		result[i] = Total{id.String(), totals[id]}
	}
	writeJSON(w, 200, result)
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("linkId"))
	if err != nil {
		apiError(w, 400, "VALIDATION_ERROR", "Parâmetro inválido: linkId")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	cached, _, cacheErr := a.cache.Get(ctx, id)
	if cacheErr == nil && len(cached) != 0 && json.Valid(cached) {
		recordCache(ctx, "hit")
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
		return
	}
	// Requisições concorrentes pelo mesmo link compartilham a consulta de cache miss.
	// O trabalho tem prazo próprio: cancelar um cliente não derruba todos os demais.
	result := a.flights.DoChan(id.String(), func() (any, error) {
		queryCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer stop()
		data, version, err := a.cache.Get(queryCtx, id)
		if err == nil && len(data) != 0 && json.Valid(data) {
			return data, nil
		}
		cacheAvailable := err == nil
		summary, err := a.repo.Summary(queryCtx, id)
		if err != nil {
			return nil, err
		}
		data, err = json.Marshal(summary)
		if err != nil {
			return nil, err
		}
		if cacheAvailable {
			if err := a.cache.Set(queryCtx, id, version, data); err != nil {
				slog.WarnContext(queryCtx, "Falha ao gravar cache", "error", err)
			}
		}
		return data, nil
	})
	select {
	case response := <-result:
		if response.Err != nil {
			slog.ErrorContext(ctx, "Falha ao consultar resumo", "error", response.Err)
			apiError(w, 500, "INTERNAL_ERROR", "Erro interno")
			return
		}
		recordCache(ctx, "miss")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response.Val.([]byte))
	case <-ctx.Done():
		apiError(w, 500, "INTERNAL_ERROR", "Erro interno")
	}
}
