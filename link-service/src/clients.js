import { Kafka } from 'kafkajs';
import pg from 'pg';
import { createClient } from 'redis';

import { createLinkRepository, initializeDatabase } from './repository.js';

const { Pool } = pg;

export async function createDependencies(env = process.env) {
  const pool = new Pool({ connectionString: env.DATABASE_URL });
  const cache = createClient({ url: env.REDIS_URL });
  const kafka = new Kafka({ brokers: env.KAFKA_BROKERS.split(',').map((broker) => broker.trim()) });
  const producer = kafka.producer();

  try {
    await initializeDatabase(pool);
    cache.on('error', (error) => console.error('Redis client error', error));
    await cache.connect();
    await producer.connect();
  } catch (error) {
    await Promise.allSettled([pool.end(), cache.isOpen ? cache.quit() : Promise.resolve(), producer.disconnect()]);
    throw error;
  }

  return {
    db: createLinkRepository(pool),
    cache,
    publisher: {
      async publish(event) {
        await producer.send({
          topic: env.KAFKA_TOPIC_LINK_CLICKED,
          messages: [{ key: event.linkId, value: JSON.stringify(event) }],
        });
      },
    },
    async close() {
      await Promise.all([pool.end(), cache.quit(), producer.disconnect()]);
    },
  };
}
