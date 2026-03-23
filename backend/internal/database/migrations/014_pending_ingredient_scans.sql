CREATE TABLE IF NOT EXISTS pending_ingredient_scans (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    amount     NUMERIC NOT NULL DEFAULT 0,
    unit       TEXT NOT NULL DEFAULT '',
    confident  BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
