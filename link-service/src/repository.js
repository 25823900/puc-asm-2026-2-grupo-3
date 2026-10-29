export async function initializeDatabase(pool) {
  await pool.query(`
    CREATE TABLE IF NOT EXISTS links (
      id UUID PRIMARY KEY,
      slug VARCHAR(7) NOT NULL UNIQUE,
      target_url VARCHAR(2048) NOT NULL,
      owner_id UUID NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      CONSTRAINT links_slug_format CHECK (slug ~ '^[0-9a-zA-Z]{7}$')
    )
  `);
  await pool.query('CREATE INDEX IF NOT EXISTS links_owner_created_idx ON links (owner_id, created_at DESC)');
  await pool.query('CREATE INDEX IF NOT EXISTS links_slug_idx ON links (slug)');
}

export function createLinkRepository(pool) {
  return {
    async insert({ id, slug, ownerId, targetUrl }) {
      const result = await pool.query(
        `INSERT INTO links (id, slug, target_url, owner_id)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (slug) DO NOTHING
         RETURNING id, slug, target_url AS "targetUrl", owner_id AS "ownerId", created_at AS "createdAt"`,
        [id, slug, targetUrl, ownerId],
      );
      return result.rows[0] ?? null;
    },

    async listByOwner(ownerId) {
      const result = await pool.query(
        `SELECT id, slug, target_url AS "targetUrl", owner_id AS "ownerId", created_at AS "createdAt"
         FROM links WHERE owner_id = $1 ORDER BY created_at DESC, id`,
        [ownerId],
      );
      return result.rows;
    },

    async findByIdForOwner(id, ownerId) {
      const result = await pool.query(
        `SELECT id, slug, target_url AS "targetUrl", owner_id AS "ownerId", created_at AS "createdAt"
         FROM links WHERE id = $1 AND owner_id = $2`,
        [id, ownerId],
      );
      return result.rows[0] ?? null;
    },

    async deleteByIdForOwner(id, ownerId) {
      const result = await pool.query(
        'DELETE FROM links WHERE id = $1 AND owner_id = $2 RETURNING slug',
        [id, ownerId],
      );
      return result.rows[0] ?? null;
    },

    async findBySlug(slug) {
      const result = await pool.query(
        `SELECT id, slug, target_url AS "targetUrl"
         FROM links WHERE slug = $1`,
        [slug],
      );
      return result.rows[0] ?? null;
    },
  };
}
