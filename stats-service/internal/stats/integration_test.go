package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

func integrationRepo(t *testing.T) *PostgresRepository {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + uuid.New().String()[:8]
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	return &PostgresRepository{pool}
}

func TestKafkaBatchReplayWithRealPostgres(t *testing.T) {
	brokers := os.Getenv("TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("TEST_KAFKA_BROKERS não definida")
	}
	r := integrationRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	broker := strings.Split(brokers, ",")[0]
	conn, err := kafka.DialContext(ctx, "tcp", broker)
	if err != nil {
		t.Fatal(err)
	}
	controller, err := conn.Controller()
	_ = conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := kafka.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	topic := "stats-test-" + uuid.NewString()
	if err := admin.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 2, ReplicationFactor: 1}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.DeleteTopics(topic) }()
	writer := &kafka.Writer{Addr: kafka.TCP(strings.Split(brokers, ",")...), Topic: topic, RequiredAcks: kafka.RequireAll}
	defer func() { _ = writer.Close() }()
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: strings.Split(brokers, ","), Topic: topic, GroupID: topic, StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: 100 * time.Millisecond})
	consumeCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); consume(consumeCtx, reader, r, &fakeCache{}) }()
	defer func() { stop(); _ = reader.Close(); <-done }()
	id := uuid.New()
	messages := make([]kafka.Message, 0, 601)
	for range 300 {
		data, _ := json.Marshal(map[string]string{"eventId": uuid.NewString(), "linkId": id.String(), "slug": "aB3xK9q", "clickedAt": "2026-10-07T22:30:00-03:00"})
		m := kafka.Message{Key: []byte(id.String()), Value: data}
		messages = append(messages, m, m)
	}
	messages = append(messages, kafka.Message{Value: []byte("invalid JSON")})
	if err := writer.WriteMessages(ctx, messages...); err != nil {
		t.Fatal(err)
	}
	for {
		result, err := r.Summary(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if result.TotalClicks == 300 {
			if len(result.ClicksPerDay) != 1 || result.ClicksPerDay[0].Date != "2026-10-08" {
				t.Fatal(result)
			}
			break
		}
		if !waitRetry(ctx, 50*time.Millisecond) {
			t.Fatalf("total=%d, want 300: %v", result.TotalClicks, ctx.Err())
		}
	}
}

func TestPostgresMigrationBatchDedupUTCAndAtomicity(t *testing.T) {
	r := integrationRepo(t)
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `CREATE TABLE click_events(event_id uuid PRIMARY KEY,link_id uuid NOT NULL,slug varchar(255) NOT NULL,clicked_at timestamptz NOT NULL); INSERT INTO click_events(event_id,link_id,slug,clicked_at) VALUES('11111111-1111-4111-8111-111111111111','7b1f6a0e-3a52-4c1e-9d0b-2f4f6f0c8a11','legacy-slug','2026-10-07T22:30:00-03:00')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	summary, err := r.Summary(ctx, linkA)
	if err != nil || summary.TotalClicks != 1 || summary.ClicksPerDay[0].Date != "2026-10-08" {
		t.Fatal(summary, err)
	}
	click, _ := parseClick(validMessage().Value)
	newClick := click
	newClick.EventID = uuid.New()
	newClick.ClickedAt = newClick.ClickedAt.Add(-24 * time.Hour)
	if err = r.InsertBatch(ctx, []Click{click, newClick, newClick}); err != nil {
		t.Fatal(err)
	}
	totals, err := r.Totals(ctx, []uuid.UUID{linkA, linkB})
	if err != nil || totals[linkA] != 2 || totals[linkB] != 0 {
		t.Fatal(totals, err)
	}
	// Um erro SQL no lote deve desfazer histórico e agregado.
	bad := newClick
	bad.EventID = uuid.New()
	bad.Slug = strings.Repeat("x", 256)
	another := newClick
	another.EventID = uuid.New()
	if err = r.InsertBatch(ctx, []Click{another, bad}); err == nil {
		t.Fatal("expected SQL failure")
	}
	var n int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM click_events`).Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	// Várias gravações concorrentes no mesmo contador não podem perder incrementos.
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			c := newClick
			c.EventID = uuid.New()
			if err := r.InsertBatch(ctx, []Click{c}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	totals, err = r.Totals(ctx, []uuid.UUID{linkA})
	if err != nil || totals[linkA] != 42 {
		t.Fatal(totals, err)
	}
}

func TestRedisVersionPreventsStaleRefill(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL não definida")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer func() { _ = client.Close() }()
	c := &RedisCache{client}
	id := uuid.New()
	ctx := context.Background()
	t.Cleanup(func() { client.Del(ctx, cacheKey(id), versionKey(id)) })
	data, version, err := c.Get(ctx, id)
	if err != nil || len(data) != 0 || version != "0" {
		t.Fatal(string(data), version, err)
	}
	if err = c.Invalidate(ctx, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	if err = c.Set(ctx, id, version, []byte(`{"totalClicks":0}`)); err != nil {
		t.Fatal(err)
	}
	data, version, err = c.Get(ctx, id)
	if err != nil || len(data) != 0 || version != "1" {
		t.Fatal(string(data), version, err)
	}
	if err = c.Set(ctx, id, version, []byte(`{"totalClicks":1}`)); err != nil {
		t.Fatal(err)
	}
	data, _, err = c.Get(ctx, id)
	if err != nil || string(data) != `{"totalClicks":1}` {
		t.Fatal(string(data), err)
	}
	ttl := client.TTL(ctx, cacheKey(id)).Val()
	if ttl <= 0 || ttl > summaryTTL {
		t.Fatal(ttl)
	}
	if err = c.Invalidate(ctx, []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	data, _, err = c.Get(ctx, id)
	if err != nil || len(data) != 0 {
		t.Fatal(string(data), err)
	}
}
