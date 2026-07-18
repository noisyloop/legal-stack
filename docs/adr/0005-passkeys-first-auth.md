# ADR-0005: Passkeys-first authentication

Status: Accepted · Date: 2026-07-18

## Context

Passwords are the dominant breach vector (phishing, reuse, stuffing). Our
users (attorneys, clients, artists) are high-value phishing targets. WebAuthn
passkeys are phishing-resistant (origin-bound), breach-resistant (no shared
secret on the server), and now have mainstream platform support with cloud
sync.

## Decision

- **Primary credential: passkeys (WebAuthn)** — resident keys preferred,
  user verification required. The server stores only public keys.
- **No password authentication at all.** We never build the password column,
  so we can never breach it. This is the strongest simplification the
  security model gets.
- **Account recovery**: single-use recovery codes (Argon2id-hashed at rest)
  issued at enrollment + admin-mediated recovery with identity verification
  for enterprise orgs. Email magic links only as an org-level opt-in for
  client-portal users, rate-limited and short-lived.
- **MFA semantics**: a passkey with user verification (biometric/PIN) is
  two factors (possession + inherence/knowledge). Additional device
  enrollment is encouraged; sign-count and AAGUID anomalies are audited.
- **Lockout & throttling**: per-account and per-IP sliding-window limits on
  ceremony starts; lockout events are audited and notify the user.

## Consequences

- ✅ Phishing-resistant by construction; no credential-stuffing surface.
- ⚠️ Recovery UX is the hard part and is designed up front (codes + admin
  path) — losing all authenticators must not mean losing the account, and
  recovery must not become the weak back door (hence rate limits + audit +
  step-up verification).
- ⚠️ Requires WebAuthn-capable clients — acceptable for a professional SaaS
  in 2026.
