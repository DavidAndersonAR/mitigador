CREATE TABLE IF NOT EXISTS incidents (
    id           TEXT PRIMARY KEY,
    host_ip      INET NOT NULL,
    vector       TEXT NOT NULL CHECK (vector IN ('udp_flood', 'icmp_flood')),
    hostgroup_id BIGINT REFERENCES hostgroups(id) ON DELETE SET NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    ended_at     TIMESTAMPTZ,
    peak_pps     BIGINT NOT NULL DEFAULT 0,
    peak_bps     BIGINT NOT NULL DEFAULT 0,
    score        DOUBLE PRECISION NOT NULL DEFAULT 0,
    details      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS incidents_started_at_idx ON incidents (started_at DESC);
CREATE INDEX IF NOT EXISTS incidents_host_ip_idx    ON incidents (host_ip);
CREATE INDEX IF NOT EXISTS incidents_active_idx     ON incidents (ended_at) WHERE ended_at IS NULL;
