# Threat Model (STRIDE)

Living document. Re-reviewed at every phase; each new feature ships with a
feature-level threat model appended to its design doc.

## Assets (what we protect, in priority order)

1. **Client-attorney privileged content** — documents, messages, audio
   masters, contract terms. Confidentiality breach is existential.
2. **Tenant isolation** — org A must never see org B's existence, let alone
   data.
3. **Audit-trail integrity** — the platform's evidentiary value.
4. **Authentication material** — sessions, passkey records, recovery codes.
5. **Availability during legal deadlines** — courts don't extend for outages.

## Trust boundaries

```
Internet ──► Edge (CDN/WAF/NGINX) ──► Go API ──► {PostgreSQL, Redis, S3, KMS}
                       ▲                  │
   Untrusted user input│                  └──► Workers (AV scan, jobs)
                       │                            ▲
                 Uploaded files ────────────────────┘ (untrusted content!)
```

Everything left of the API is untrusted. Uploaded file *content* remains
untrusted even after storage (it crosses back at preview/AV-scan time).
Zero Trust internally: workers and future services authenticate to the API
like any client; the network is not a permission.

## STRIDE analysis

### Spoofing
| Threat | Mitigation |
|---|---|
| Credential phishing | Passkeys only — origin-bound, unphishable (ADR-0005) |
| Session theft (XSS, malware) | httpOnly `__Host-` cookies; sessions bound to device metadata with anomaly audit; instant revocation (ADR-0004) |
| Session fixation | New session ID minted at every privilege change |
| Recovery-path abuse | Argon2id-hashed single-use codes, rate limits, audit + user notification on use |
| Spoofed internal callers | mTLS/service tokens when services split; no network-trust assumptions |

### Tampering
| Threat | Mitigation |
|---|---|
| Audit-log modification (incl. insider/DBA) | Append-only trigger + least-priv DB role + per-org hash chain + WORM checkpoints (ADR-0006) |
| Document substitution | SHA-256 plaintext hash per version; versions immutable — edits create new versions |
| Storage-layer bit tampering | AES-GCM authenticated encryption (ADR-0008) |
| Migration/history rewrite | Checksummed migrations verified at boot (ADR-0011) |
| Supply-chain tampering | Minimal deps, `go.sum`/lockfiles, dependency review, SBOM, pinned+signed containers |

### Repudiation
| Threat | Mitigation |
|---|---|
| "I never sent/approved that" | Same-transaction audit with actor, IP, device, request ID (ADR-0006) |
| Clock games | Server-side timestamps only; NTP-synced hosts |

### Information disclosure
| Threat | Mitigation |
|---|---|
| Cross-tenant leak | Triple layer: app scoping + RLS + audit alerting (ADR-0003) |
| IDOR | Unguessable UUIDv7 IDs **plus** authZ check on every object access (IDs are never the control) |
| Bucket exposure / stolen storage creds | Envelope encryption — bucket holds ciphertext (ADR-0008) |
| DB dump exposure | Session/recovery tokens stored hashed; documents not in DB; field-level encryption for highest-sensitivity columns |
| Secrets in logs/audit | Central redaction in logging + audit serializers; log-scanning tests |
| Error/stack leakage | RFC 7807 responses, generic 5xx; details only in server logs |
| AI assistant cross-org leakage | Retrieval passes the same authz engine per-chunk; per-org index partitions; no cross-org training |
| Traffic analysis of URLs | IDs opaque; signed URLs short-lived, single-object |

### Denial of service
| Threat | Mitigation |
|---|---|
| Credential/ceremony flooding | Per-IP + per-account rate limits, WAF |
| Large-body/zip-bomb uploads | Body size caps, multipart part limits, decompression bombs never auto-expanded, AV scan sandboxed |
| Expensive-query abuse | Cursor pagination (no deep OFFSET), statement timeouts, per-principal rate limiting |
| WebSocket exhaustion | Per-user connection caps, idle timeouts, backpressure |

### Elevation of privilege
| Threat | Mitigation |
|---|---|
| Missing authZ on an endpoint | Deny-by-default: handlers must declare (action, resource); central engine; permission test suite runs the matrix |
| Role confusion across orgs | Permissions always evaluated in the (user, org) membership context, never global |
| Time-boxed grant outliving intent | Grants carry expiry evaluated at check time, not at grant time |
| SQL injection | Parameterized queries only (pgx); no string-built SQL; gosec in CI |
| SSRF from user-supplied URLs | Outbound fetch allowlist; metadata endpoints blocked; fetchers run in workers with egress policy |
| Malicious uploads (polyglot files, XSS via SVG/HTML) | Content-type sniffing + allowlist, `Content-Disposition: attachment`, separate download origin, CSP, AV pipeline |
| Container/host escape | Distroless non-root images, read-only FS, dropped capabilities, image scanning |

## Abuse cases we explicitly design against

- A departing employee mass-downloading matter documents → rate/volume
  anomaly detection on document access (the KMS unwrap log is the backstop).
- An org admin quietly adding themselves to a privileged matter → approval
  workflow for permission grants + audit visibility to other admins.
- A compromised reverse proxy injecting headers → the API only trusts
  `X-Forwarded-For` from configured proxy CIDRs.

## Residual risks (accepted, for now)

- Server-side encryption means the operator *can* technically read content
  (keys live in KMS we control). True E2EE for messaging is a Phase 6+
  investigation; it conflicts with server-side search/AI features and is a
  product decision, not just an engineering one.
- Single-region deployment until Phase 9 (availability risk documented in
  the deployment doc).
