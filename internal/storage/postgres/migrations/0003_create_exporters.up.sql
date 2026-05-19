CREATE TABLE IF NOT EXISTS exporters (
    id                   BIGSERIAL PRIMARY KEY,
    source_ip            INET UNIQUE NOT NULL,
    type                 TEXT NOT NULL CHECK (type IN ('netflow', 'ipfix', 'sflow')),
    sample_rate_override INTEGER NOT NULL DEFAULT 0,
    description          TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS exporters_type_idx ON exporters(type);
