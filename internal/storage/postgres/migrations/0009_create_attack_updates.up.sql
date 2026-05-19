CREATE TABLE IF NOT EXISTS attack_updates (
    id           BIGSERIAL PRIMARY KEY,
    incident_id  TEXT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    observed_at  TIMESTAMPTZ NOT NULL,
    pps          BIGINT NOT NULL,
    bps          BIGINT NOT NULL,
    score        DOUBLE PRECISION NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('started', 'update', 'ended'))
);
CREATE INDEX IF NOT EXISTS attack_updates_incident_idx ON attack_updates (incident_id, observed_at);
