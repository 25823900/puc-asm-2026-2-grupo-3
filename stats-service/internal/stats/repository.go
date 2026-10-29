package stats

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

// A migração preserva click_events do serviço Python e agrega o histórico uma vez.
// O advisory lock serializa instâncias e o lock da tabela bloqueia inserts antigos
// durante o backfill. Depois do corte, todos os produtores deste banco devem usar Go.
func (r *PostgresRepository) Migrate(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(72610328);
		CREATE TABLE IF NOT EXISTS click_events (
		 event_id uuid PRIMARY KEY, link_id uuid NOT NULL, slug varchar(255) NOT NULL,
		 clicked_at timestamptz NOT NULL);
		CREATE INDEX IF NOT EXISTS clicks_link_clicked_idx ON click_events(link_id, clicked_at);
		CREATE TABLE IF NOT EXISTS stats_migrations (version integer PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS link_daily_counts (
		 link_id uuid NOT NULL, day date NOT NULL, clicks bigint NOT NULL,
		 PRIMARY KEY(link_id, day));`)
	if err != nil {
		return err
	}
	var migrated bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stats_migrations WHERE version=1)`).Scan(&migrated); err != nil {
		return err
	}
	if !migrated {
		_, err = tx.Exec(ctx, `
		 LOCK TABLE click_events IN SHARE ROW EXCLUSIVE MODE;
		 INSERT INTO link_daily_counts(link_id, day, clicks)
		 SELECT link_id, (clicked_at AT TIME ZONE 'UTC')::date, count(*) FROM click_events GROUP BY 1, 2;
		 INSERT INTO stats_migrations(version) VALUES(1);`)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) InsertBatch(ctx context.Context, clicks []Click) error {
	if len(clicks) == 0 {
		return nil
	}
	ctx, span := otel.Tracer("stats.repository").Start(ctx, "postgres insert clicks batch")
	defer span.End()
	events := make([]uuid.UUID, len(clicks))
	links := make([]uuid.UUID, len(clicks))
	slugs := make([]string, len(clicks))
	times := make([]time.Time, len(clicks))
	for i, c := range clicks {
		events[i], links[i], slugs[i], times[i] = c.EventID, c.LinkID, c.Slug, c.ClickedAt
	}
	// Uma ida ao banco por lote. Somente eventos novos incrementam os contadores;
	// histórico e agregados são gravados atomicamente pela mesma instrução SQL.
	_, err := r.pool.Exec(ctx, `
	 WITH inserted AS (
	  INSERT INTO click_events(event_id, link_id, slug, clicked_at)
	  SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::timestamptz[])
	  ON CONFLICT(event_id) DO NOTHING RETURNING link_id, clicked_at
	 )
	 INSERT INTO link_daily_counts(link_id, day, clicks)
	 SELECT link_id, (clicked_at AT TIME ZONE 'UTC')::date, count(*) FROM inserted
	 GROUP BY 1, 2 ORDER BY 1, 2
	 ON CONFLICT(link_id, day) DO UPDATE
	 SET clicks=link_daily_counts.clicks + EXCLUDED.clicks`, events, links, slugs, times)
	return err
}

func (r *PostgresRepository) Totals(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	ctx, span := otel.Tracer("stats.repository").Start(ctx, "postgres totals")
	defer span.End()
	rows, err := r.pool.Query(ctx, `SELECT link_id, sum(clicks)::bigint FROM link_daily_counts WHERE link_id=ANY($1::uuid[]) GROUP BY link_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := make(map[uuid.UUID]int64, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		totals[id] = n
	}
	return totals, rows.Err()
}

func (r *PostgresRepository) Summary(ctx context.Context, id uuid.UUID) (Summary, error) {
	ctx, span := otel.Tracer("stats.repository").Start(ctx, "postgres daily summary")
	defer span.End()
	result := Summary{LinkID: id.String(), ClicksPerDay: make([]DayCount, 0)}
	rows, err := r.pool.Query(ctx, `SELECT day, clicks FROM link_daily_counts WHERE link_id=$1 ORDER BY day`, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		var n int64
		if err := rows.Scan(&day, &n); err != nil {
			return result, err
		}
		result.TotalClicks += n
		result.ClicksPerDay = append(result.ClicksPerDay, DayCount{day.Format("2006-01-02"), n})
	}
	return result, rows.Err()
}
