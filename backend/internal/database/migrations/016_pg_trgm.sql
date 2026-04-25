-- Enable pg_trgm for indexed substring (ILIKE '%foo%') search.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Trigram GIN indexes serve LibrarySearch's per-keyword ILIKE filters.
-- Without them, every keystroke triggered a sequential scan + JSONB extraction.
CREATE INDEX IF NOT EXISTS idx_recipes_title_trgm
    ON recipes USING GIN (title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_recipes_description_trgm
    ON recipes USING GIN (description gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_recipes_cuisine_trgm
    ON recipes USING GIN (cuisine_type gin_trgm_ops);
