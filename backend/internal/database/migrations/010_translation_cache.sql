CREATE TABLE IF NOT EXISTS translation_cache (
    source_text   TEXT        NOT NULL,
    target_lang   TEXT        NOT NULL,
    translated_text TEXT      NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_text, target_lang)
);
