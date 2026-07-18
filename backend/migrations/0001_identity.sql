-- 0001_identity: tenancy root (organizations), global user accounts, and the
-- membership join that anchors RBAC. See docs/database/schema.md and ADR-0003.

CREATE EXTENSION IF NOT EXISTS citext;

-- updated_at is trigger-maintained so application code cannot forget it.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END $$;

CREATE TABLE organizations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        citext      NOT NULL,
    name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind        text        NOT NULL CHECK (kind IN ('law_firm', 'client_org', 'management', 'label', 'personal')),
    status      text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'offboarding', 'closed')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    deleted_by  uuid,
    CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$')
);

-- Partial unique index: a deleted org's slug can be reclaimed (ADR-0010).
CREATE UNIQUE INDEX organizations_slug_live_uq
    ON organizations (slug) WHERE deleted_at IS NULL;

CREATE TRIGGER organizations_set_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE users (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email              citext      NOT NULL CHECK (length(email) <= 320 AND position('@' IN email) > 1),
    email_verified_at  timestamptz,
    display_name       text        NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    status             text        NOT NULL DEFAULT 'invited' CHECK (status IN ('invited', 'active', 'suspended', 'deactivated')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz,
    deleted_by         uuid
    -- Deliberately no password column: passkeys only (ADR-0005).
);

CREATE UNIQUE INDEX users_email_live_uq
    ON users (email) WHERE deleted_at IS NULL;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE org_memberships (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid        NOT NULL REFERENCES organizations (id) ON DELETE RESTRICT,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    role        text        NOT NULL CHECK (role IN ('org_owner', 'org_admin', 'attorney', 'staff', 'client', 'guest')),
    status      text        NOT NULL DEFAULT 'invited' CHECK (status IN ('invited', 'active', 'suspended')),
    invited_by  uuid        REFERENCES users (id) ON DELETE RESTRICT,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    removed_at  timestamptz
);

-- One live membership per user per org; historical (removed) rows are kept.
CREATE UNIQUE INDEX org_memberships_live_uq
    ON org_memberships (org_id, user_id) WHERE removed_at IS NULL;
CREATE INDEX org_memberships_user_idx ON org_memberships (user_id);
CREATE INDEX org_memberships_org_idx ON org_memberships (org_id);
CREATE INDEX org_memberships_invited_by_idx ON org_memberships (invited_by);

CREATE TRIGGER org_memberships_set_updated_at
    BEFORE UPDATE ON org_memberships
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
