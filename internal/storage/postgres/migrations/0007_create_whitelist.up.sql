-- P1: table created, not enforced.
-- P2 (SAFE-01) will refuse RTBH announcements for any /32 contained in any row's prefix.
CREATE TABLE IF NOT EXISTS whitelist (
    id         BIGSERIAL PRIMARY KEY,
    prefix     CIDR UNIQUE NOT NULL,
    reason     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
