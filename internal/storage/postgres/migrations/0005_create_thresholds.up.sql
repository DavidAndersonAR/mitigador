CREATE TABLE IF NOT EXISTS thresholds (
    id              BIGSERIAL PRIMARY KEY,
    hostgroup_id    BIGINT NOT NULL REFERENCES hostgroups(id) ON DELETE CASCADE,
    vector          TEXT NOT NULL CHECK (vector IN ('udp_flood', 'icmp_flood')),
    pps             BIGINT NOT NULL CHECK (pps > 0),
    bps             BIGINT NOT NULL CHECK (bps > 0),
    min_window_sec  INTEGER NOT NULL DEFAULT 5 CHECK (min_window_sec > 0),
    grace_sec       INTEGER NOT NULL DEFAULT 60 CHECK (grace_sec > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (hostgroup_id, vector)
);
