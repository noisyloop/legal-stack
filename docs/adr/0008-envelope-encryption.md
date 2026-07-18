# ADR-0008: Envelope encryption with a KMS abstraction

Status: Accepted · Date: 2026-07-18

## Context

Legal documents and audio masters are the crown jewels. "Encryption at rest"
from the storage provider protects against stolen disks, not against a
compromised bucket credential or a misconfigured bucket policy. We need
application-layer encryption with real key management — without binding the
codebase to one vendor's KMS.

## Decision

**Envelope encryption** for every stored object:

1. Each object (each document *version*) gets a fresh random 256-bit
   **Data Encryption Key (DEK)**; content is encrypted with
   AES-256-GCM (chunked, so streaming uploads/downloads don't buffer whole
   files; each chunk gets a derived nonce and the sequence number is
   authenticated to prevent chunk reordering).
2. The DEK is encrypted (**wrapped**) by a **Key Encryption Key (KEK)** held
   in a KMS. Only the wrapped DEK is stored, alongside object metadata.
3. KEKs are **per-organization**, enabling per-tenant rotation and true
   crypto-shredding: destroy an org's KEK material and its data is
   irrecoverable — this is how "delete on offboarding" becomes provable.
4. A `KeyManagement` **port** with implementations: local (file/dev),
   HashiCorp Vault transit, cloud KMS (AWS/GCP). The domain never sees a
   raw KEK — only `Wrap`/`Unwrap` operations, so an HSM-backed KMS slots in
   unchanged.
5. SHA-256 of the **plaintext** is computed during upload and stored for
   integrity/dedup/e-discovery; ciphertext integrity comes from GCM tags.

## Consequences

- ✅ Bucket compromise yields ciphertext; key compromise requires a second,
  separately-audited system (KMS).
- ✅ Rotation = re-wrap DEKs (cheap, metadata-only), not re-encrypt content.
- ⚠️ Every download needs a KMS `Unwrap` — cached with short TTL per session,
  and rate-limited, because the KMS call log *is* the object-access log of
  last resort.
- ⚠️ Search over encrypted content must happen at index time in the app
  (extract text → index), never by the datastore.
