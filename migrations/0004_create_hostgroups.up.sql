CREATE TABLE IF NOT EXISTS hostgroups (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT UNIQUE NOT NULL,
    prefix      CIDR NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS hostgroups_prefix_idx ON hostgroups USING gist (prefix inet_ops);
