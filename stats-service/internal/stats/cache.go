package stats

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const cacheTimeout = 100 * time.Millisecond
const summaryTTL = 30 * time.Second

type RedisCache struct{ client *redis.Client }

func cacheKey(id uuid.UUID) string   { return "stats:summary:v2:" + id.String() }
func versionKey(id uuid.UUID) string { return "stats:version:v2:" + id.String() }

func (c *RedisCache) Get(ctx context.Context, id uuid.UUID) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()
	// MGET obtém conteúdo e versão no mesmo instante, com uma ida ao Redis.
	values, err := c.client.MGet(ctx, cacheKey(id), versionKey(id)).Result()
	if err != nil {
		return nil, "", err
	}
	version := "0"
	if values[1] != nil {
		version = values[1].(string)
	}
	if values[0] == nil {
		return nil, version, nil
	}
	return []byte(values[0].(string)), version, nil
}

var setSummary = redis.NewScript(`
 if (redis.call('GET', KEYS[2]) or '0') == ARGV[1] then
  return redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
 end
 return nil`)

func (c *RedisCache) Set(ctx context.Context, id uuid.UUID, version string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()
	err := setSummary.Run(ctx, c.client, []string{cacheKey(id), versionKey(id)}, version, data, int(summaryTTL.Seconds())).Err()
	if err == redis.Nil {
		return nil
	} // Uma invalidação venceu a consulta: não republicar.
	return err
}

func (c *RedisCache) Invalidate(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()
	_, err := c.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, id := range ids {
			// EVAL em pipeline evita o round-trip extra de EVALSHA/NOSCRIPT.
			pipe.Eval(ctx, invalidateSummarySource, []string{cacheKey(id), versionKey(id)})
		}
		return nil
	})
	return err
}

const invalidateSummarySource = `redis.call('INCR', KEYS[2]); redis.call('EXPIRE', KEYS[2], 60); return redis.call('DEL', KEYS[1])`
