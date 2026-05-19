-- mikrotik_routers — source of truth for the subscriber-resolver poller.
-- Replaces the static `mikrotik.routers` array in config.yaml so operators
-- can manage routers through the dashboard UI at runtime.
--
-- Security: `password` is stored as plaintext for now; rotate to a column
-- encrypted with pgcrypto + a key from session_secret in a follow-up.
-- File permissions on the Postgres data dir (mode 0700, owner postgres)
-- are the current line of defence — document this in the deploy guide.

CREATE TABLE mikrotik_routers (
    id          BIGSERIAL    PRIMARY KEY,
    name        TEXT         NOT NULL UNIQUE,
    url         TEXT         NOT NULL,
    username    TEXT         NOT NULL,
    password    TEXT         NOT NULL,
    verify_tls  BOOLEAN      NOT NULL DEFAULT FALSE,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX mikrotik_routers_enabled_idx
    ON mikrotik_routers (enabled)
    WHERE enabled = TRUE;
