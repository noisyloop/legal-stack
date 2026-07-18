-- 0003_audit: append-only, hash-chained audit log (ADR-0006).
-- Rows are written in the same transaction as the state change they record.
-- Tamper evidence: hash = SHA-256(prev_hash || canonical(event)), chained
-- per organization (system-wide events chain under the zero UUID).

CREATE TABLE audit_log (
    seq             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id              uuid        NOT NULL UNIQUE,
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    org_id          uuid        REFERENCES organizations (id) ON DELETE RESTRICT,
    org_seq         bigint      NOT NULL CHECK (org_seq >= 1),
    matter_id       uuid,
    actor_type      text        NOT NULL CHECK (actor_type IN ('user', 'system', 'service')),
    actor_user_id   uuid        REFERENCES users (id) ON DELETE RESTRICT,
    action          text        NOT NULL CHECK (length(action) BETWEEN 1 AND 100),
    resource_type   text        NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 100),
    resource_id     uuid,
    -- json (not jsonb) on purpose: jsonb normalizes key order and number
    -- formatting, so stored bytes would differ from the bytes that were
    -- hashed at append time and chain verification would break. json stores
    -- the text verbatim; cast to jsonb at query time when needed.
    old_values      json,
    new_values      json,
    metadata        jsonb       NOT NULL DEFAULT '{}',
    ip              inet,
    user_agent      text,
    request_id      text        CHECK (request_id IS NULL OR length(request_id) <= 64),
    correlation_id  text        CHECK (correlation_id IS NULL OR length(correlation_id) <= 64),
    prev_hash       bytea       NOT NULL CHECK (length(prev_hash) = 32),
    hash            bytea       NOT NULL CHECK (length(hash) = 32)
);

-- Chain position must be unique per chain. NULL org (system events) chains
-- under the zero UUID, so the expression index covers both cases.
CREATE UNIQUE INDEX audit_log_chain_seq_uq
    ON audit_log (COALESCE(org_id, '00000000-0000-0000-0000-000000000000'::uuid), org_seq);

-- The four real query shapes: by org+time, by actor, by resource, by request.
CREATE INDEX audit_log_org_time_idx      ON audit_log (org_id, occurred_at DESC);
CREATE INDEX audit_log_actor_time_idx    ON audit_log (actor_user_id, occurred_at DESC);
CREATE INDEX audit_log_resource_idx      ON audit_log (resource_type, resource_id);
CREATE INDEX audit_log_request_idx       ON audit_log (request_id);

-- Append-only enforcement. Belt: this trigger. Suspenders: the runtime DB
-- role is granted only INSERT/SELECT on this table (provisioning docs).
CREATE FUNCTION audit_log_block_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'raise_exception';
END $$;

CREATE TRIGGER audit_log_immutable
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_block_mutation();

-- TRUNCATE bypasses row-level triggers; block it explicitly.
CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_block_mutation();
