# Storage and token formats

This document specifies the formats Chaintrail writes or exchanges. It is written for maintainers, forensic review, backup validation, and compatibility analysis.

## Journal directory

A journal directory contains:

```text
.chaintrail/
├── meta.json
├── journal.ndjson
├── head.json
└── journal.lock
```

Only `meta.json` and `journal.ndjson` are authoritative history. `head.json` is derivative cache state. `journal.lock` is coordination state.

## `meta.json`

Example:

```json
{
  "version": 1,
  "journal_id": "7f7d0d523ab841819c2a2a02bfbd2c89",
  "created_at": "2026-09-08T12:00:00Z"
}
```

Fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `version` | integer | Storage-format version. Current value: `1`. |
| `journal_id` | string | 16 random bytes encoded as 32 lowercase hexadecimal characters. |
| `created_at` | string | UTC RFC3339Nano timestamp. |

The file is immutable after successful initialization. Any change modifies the metadata anchor and invalidates the journal chain.

## Metadata anchor

The initial previous-hash value is:

```text
SHA256(
  "chaintrail-anchor-v1\n" ||
  canonical_json(meta)
)
```

The domain-separation prefix prevents the same JSON bytes from being interpreted as a record-hash input.

## `journal.ndjson`

The journal is newline-delimited JSON. Exactly one complete record occupies each line and each valid line is terminated by `\n`.

Example:

```json
{"seq":1,"time":"2026-09-08T12:00:01Z","kind":"deploy.started","payload":{"service":"api","version":"1.4.2"},"prev":"<64 hex>","hash":"<64 hex>"}
```

### Record fields

| Field | Type | Constraints |
| --- | --- | --- |
| `seq` | unsigned integer | Starts at 1 and increments by exactly 1. |
| `time` | string | RFC3339Nano timestamp produced in UTC by Chaintrail. |
| `kind` | string | 1-64 characters; first character alphanumeric; remaining characters alphanumeric or `.`, `_`, `:`, `-`. |
| `payload` | any JSON value | Input limited to 1 MiB and stored in canonical JSON form. |
| `prev` | string | 64 lowercase hexadecimal characters; metadata anchor for sequence 1, previous record hash otherwise. |
| `hash` | string | 64 lowercase hexadecimal SHA-256 digest of the record without `hash`. |

Unknown top-level record fields are rejected by verification.

### Record hash

The digest input is the canonical JSON encoding of:

```json
{
  "seq": 42,
  "time": "2026-09-08T17:42:03.981Z",
  "kind": "deploy.completed",
  "payload": {"service":"api"},
  "prev": "<previous-hash>"
}
```

The resulting digest is:

```text
SHA256(
  "chaintrail-record-v1\n" ||
  canonical_json(record_without_hash)
)
```

## Canonical JSON rules

Chaintrail’s canonicalizer exists to make hashing independent of formatting and object insertion order.

Behavior:

- object keys are sorted lexically;
- whitespace outside strings is removed;
- strings use Go JSON escaping semantics;
- arrays preserve element order;
- booleans and `null` use standard JSON literals;
- numbers are parsed as arbitrary-precision decimal components rather than binary floating point;
- numerically equivalent representations normalize to the same scientific representation.

Examples that canonicalize equivalently:

```json
{"a":1,"b":0.01}
```

```json
{ "b": 1e-2, "a": 1.00 }
```

The canonical representation is an internal hash format, not a promise that all serialized Chaintrail JSON files use scientific-number notation. `journal.ndjson` record envelopes are encoded with ordinary Go `encoding/json`; the payload bytes stored inside the envelope are canonical.

## Maximum record size

Input payloads are capped at 1 MiB. The record reader permits additional envelope overhead but rejects records beyond the implementation’s bounded maximum. This prevents a corrupted or hostile journal line from causing unbounded memory use during verification.

## `head.json`

Example:

```json
{
  "seq": 42,
  "hash": "<64 hex>",
  "size": 23871
}
```

Fields:

| Field | Meaning |
| --- | --- |
| `seq` | Sequence represented by the cache. |
| `hash` | Expected head hash or metadata anchor when sequence is zero. |
| `size` | Journal byte size observed when the cache was written. |

`head.json` is not part of the integrity proof. It may be deleted and rebuilt. Full-chain readers do not accept it as evidence that earlier records are valid.

## `journal.lock`

The lock file has no application payload. Its existence is only a stable filesystem object on which the supported Unix implementation acquires shared or exclusive advisory locks.

Copying it in a backup is harmless but not required for history preservation.

## Plain checkpoint

A plain checkpoint token is ASCII:

```text
<JOURNAL_ID>:<SEQUENCE>:<HASH>
```

Example shape:

```text
7f7d0d523ab841819c2a2a02bfbd2c89:42:4a1c...64hex
```

Validation requires:

- 32-hex-character journal ID;
- unsigned decimal sequence;
- 64-hex-character digest.

Sequence 0 refers to the metadata anchor.

## Signed checkpoint token

Signed checkpoints use three dot-separated fields:

```text
CT1.<payload-base64url>.<signature-base64url>
```

`CT1` is the token-format marker.

### Payload

Decoded payload JSON:

```json
{
  "version": 1,
  "journal_id": "7f7d0d523ab841819c2a2a02bfbd2c89",
  "seq": 42,
  "hash": "<64 hex>",
  "signed_at": "2026-09-08T18:00:00Z",
  "key_id": "<sha256-of-public-key>"
}
```

The payload is encoded using deterministic Go struct-field order and compact `encoding/json` output. Verification decodes the typed structure, rejects unknown fields/trailing data, re-encodes it, and requires exact byte equality with the token payload before checking the signature.

### Key ID

```text
key_id = hex(SHA256(raw_ed25519_public_key))
```

The full 256-bit digest is retained.

### Signature input

```text
ed25519.Sign(
  private_key,
  "chaintrail-checkpoint-signature-v1\n" || payload_json_bytes
)
```

The domain prefix distinguishes Chaintrail checkpoint signatures from signatures created by another protocol using the same key.

### Key file formats

Private key:

- Ed25519;
- PKCS#8 DER wrapped in PEM block `PRIVATE KEY`;
- generated with file mode `0600`.

Public key:

- Ed25519;
- SubjectPublicKeyInfo/PKIX DER wrapped in PEM block `PUBLIC KEY`;
- generated with file mode `0644`.

## Compatibility policy

### Format version 1

Readers currently require `meta.version == 1`. Unknown versions fail explicitly rather than being interpreted approximately.

### Adding fields

The record decoder uses `DisallowUnknownFields`; adding top-level record fields therefore requires a format-version decision and coordinated reader update.

This strictness is intentional. Audit evidence should fail closed when semantics are unknown.

### Signed token version

Signed token payloads carry their own `version`. Unknown versions fail before signature acceptance is used as a journal trust anchor.

## Backup fidelity

A byte-for-byte copy of `meta.json` and `journal.ndjson` preserves journal history. Reformatting either file is not a valid backup transformation because metadata and stored canonical payload bytes participate in verification.

`head.json` may be recreated after restore. Operators should retain external plain/signed checkpoints independently so a restored older backup can be distinguished from the expected current history.
