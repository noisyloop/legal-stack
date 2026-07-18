-- 0002_auth: passkey credentials, opaque server-side sessions, recovery
-- codes, and durable auth telemetry. See ADR-0004 / ADR-0005.
-- The server stores only verifiers (public keys, hashes) — never secrets.

CREATE TABLE webauthn_credentials (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    credential_id    bytea       NOT NULL UNIQUE,
    public_key       bytea       NOT NULL,
    sign_count       bigint      NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    transports       text[]      NOT NULL DEFAULT '{}',
    aaguid           uuid,
    nickname         text        CHECK (nickname IS NULL OR length(nickname) <= 100),
    backup_eligible  boolean     NOT NULL DEFAULT false,
    backup_state     boolean     NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at     timestamptz,
    revoked_at       timestamptz
);

CREATE INDEX webauthn_credentials_user_idx ON webauthn_credentials (user_id);

CREATE TRIGGER webauthn_credentials_set_updated_at
    BEFORE UPDATE ON webauthn_credentials
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE sessions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    -- SHA-256 of the opaque cookie token: a DB leak yields no usable tokens.
    token_hash       bytea       NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,  -- absolute lifetime cap
    idle_expires_at  timestamptz NOT NULL,  -- rolling inactivity cap
    last_seen_at     timestamptz NOT NULL DEFAULT now(),
    ip               inet,
    user_agent       text,
    device_name      text,
    revoked_at       timestamptz,
    revoke_reason    text        CHECK (revoke_reason IS NULL OR revoke_reason IN
                                        ('logout', 'logout_all', 'admin', 'lockout', 'expired_rotation', 'anomaly'))
);

-- Serves the device-management screen: live sessions per user.
CREATE INDEX sessions_user_live_idx ON sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE recovery_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    code_hash   text        NOT NULL,  -- Argon2id encoded hash
    created_at  timestamptz NOT NULL DEFAULT now(),
    used_at     timestamptz
);

CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

-- High-volume security telemetry feeding lockout decisions and the
-- user-facing security page. Distinct from audit_log (per-tenant, chained).
CREATE TABLE auth_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        REFERENCES users (id) ON DELETE RESTRICT,
    email       citext,  -- captured for attempts against unknown accounts
    event       text        NOT NULL CHECK (event IN (
                    'login_success', 'login_failure', 'logout',
                    'credential_registered', 'credential_revoked',
                    'recovery_code_used', 'lockout_started', 'lockout_cleared',
                    'session_revoked')),
    ip          inet,
    user_agent  text,
    metadata    jsonb       NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_events_user_time_idx ON auth_events (user_id, created_at DESC);
CREATE INDEX auth_events_ip_time_idx ON auth_events (ip, created_at DESC);
