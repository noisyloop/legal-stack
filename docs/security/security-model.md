# Security Model — Authentication & Authorization

## Authentication (Phase 2 implements; designed now)

### Principals
- **User** — a human, authenticated via passkey; may belong to many orgs.
- **Service** — internal worker or integration; authenticated via mTLS or
  short-lived signed tokens. Never shares the user session mechanism.

### Passkey ceremonies
- Registration and login follow WebAuthn L3: server-generated challenges
  (single-use, 5-minute TTL, stored server-side), origin and RP-ID
  validation, user-verification required.
- Sign-count regression and unexpected AAGUID transitions raise audit
  anomalies (possible credential cloning).

### Session lifecycle
```
login ─► mint 256-bit token ─► Set-Cookie __Host-session (httpOnly, Secure, SameSite=Lax)
      └► store SHA-256(token) + user, ip, ua, device, expiries

request ─► hash cookie token ─► indexed lookup ─► checks:
           not revoked · idle timeout (24h) · absolute timeout (30d,
           org-configurable) ─► principal in context

logout / device removal / lockout / role change ─► revoke row(s) + purge cache
```
Trusted-device "remember me" extends idle timeout per device after an
explicit user choice, recorded and revocable in device management.

### Account lockout
Sliding-window failure counters per account **and** per IP (Redis), with
exponential backoff rather than hard lock (hard lockout is a DoS vector
against a known victim); step-up (recovery flow) always remains available.
All lockout transitions are audited and notified.

## Authorization: RBAC × ABAC

Single entry point, deny by default:

```go
authz.Authorize(ctx, Subject{UserID, OrgID, Roles}, Action, Resource) error
```

### Layer 1 — RBAC (coarse, comprehensible)
Roles are **per-membership** (user × org), never global:

| Role | Intent |
|---|---|
| `org_owner` | Billing, org lifecycle, role administration |
| `org_admin` | User/matter administration inside the org |
| `attorney` | Full professional access to assigned matters |
| `staff` | Paralegal/assistant; matter work minus privileged actions |
| `client` | Portal access to their own matters only |
| `guest` | External counsel/collaborator; explicit per-matter grants only |

Roles map to permission sets (e.g., `matter:create`, `document:approve`).
The catalog is data, not code — auditable and org-extensible later.

### Layer 2 — ABAC (fine-grained, contextual)
RBAC answers "may this role ever do this"; ABAC answers "may this person do
this *to this resource, now*". Policies evaluate attributes:

- **Subject**: membership role, matter participation, explicit grants
- **Resource**: org, matter, sensitivity label, legal-hold flag, ACL entries
- **Action**: read / write / approve / share / delete / administer
- **Environment**: time (grant expiry, business-hours policies), IP
  reputation hooks, session age (step-up for sensitive actions)

Example policy, in words: *"A `guest` may `document:read` if an unexpired
grant exists for (guest, matter, read-or-higher) AND the document is not
labeled `attorney_only` AND the matter is not restricted."*

### Grants, time-limits, approvals
- Grants are rows: `(subject, resource, actions, granted_by, expires_at)`.
- Expiry checked at evaluation time — revocation and expiry are immediate.
- Sensitive grants (external sharing, privilege escalation) require a
  second approver: the grant is created `pending` and inert until approved;
  both steps audited.

### Enforcement rules
1. Handlers never inspect roles directly — only `authz.Authorize`.
2. Object *existence* is protected too: unauthorized reads return 404, not
   403, so resources can't be enumerated.
3. List endpoints filter by permission in the query (post-filtering leaks
   counts and paginates wrongly).
4. The permission test matrix (role × action × resource state) runs in CI;
   adding an endpoint without matrix entries fails review.

## Transport & browser hardening
- TLS 1.3 (1.2 floor at the edge), HSTS with preload.
- CSP: `default-src 'self'`, nonced scripts, `frame-ancestors 'none'`,
  user content served from a separate origin, `object-src 'none'`.
- CSRF: SameSite=Lax + double-submit token on mutations.
- Security headers set by API middleware from day one (already implemented).

## Secrets
- No secrets in the repo, images, or env-committed files; `.env.example`
  documents shape only. Gitleaks runs in CI.
- Runtime secrets come from the environment (dev) → Vault/KMS abstraction
  (prod); rotation procedures documented per secret in the runbook.
