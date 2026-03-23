CREATE TABLE IF NOT EXISTS inventory (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    amount     NUMERIC,
    unit       TEXT NOT NULL DEFAULT '',
    notes      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
