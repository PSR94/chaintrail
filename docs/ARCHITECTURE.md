# Architecture

## Purpose

Chaintrail is a local integrity journal. The architecture is optimized for four properties:

1. **The authoritative history stays simple enough to inspect manually.**
2. **A completed append is durable before derivative state is updated.**
3. **Readers that return trusted data replay the complete integrity chain.**
4. **Rollback detection can cross the machine boundary through external checkpoints.**

The design intentionally avoids a database, background daemon, network service, and runtime dependencies.

## Components

### CLI adapter — `cmd/chaintrail`

The CLI owns command parsing, payload-source selection, output formatting, and exit-code mapping. It does not implement integrity rules itself.

Commands are grouped around operational intent:

- lifecycle: `init`;
- writes: `append`;
- integrity: `verify`, `checkpoint`;
- trust-anchor management: `keygen`, signed checkpoint generation/verification;
- analysis: `query`, `stats`, `tail`;
- recovery: `repair`.

Keeping command parsing outside the library lets tests exercise integrity behavior without shelling out, while command-level tests still verify end-to-end argument and exit semantics.

### Journal engine — `journal.go`

The journal engine owns initialization, append behavior, metadata loading, head-cache validation, record encoding/decoding, hash construction, and low-level atomic writes.

The authoritative state is:

- `meta.json` — immutable journal identity and format version;
- `journal.ndjson` — authoritative ordered record stream.

Derivative/coordination state is:

- `head.json` — performance cache;
- `journal.lock` — OS lock rendezvous file.

### Canonical JSON — `canonical.go`

Record hashes cannot depend on map iteration order or insignificant JSON whitespace. Payloads and hash inputs therefore pass through a deterministic encoder.

Canonicalization:

- sorts object keys lexically;
- removes insignificant whitespace;
- recursively canonicalizes arrays/objects;
- parses numbers without floating-point loss;
- normalizes numerically equivalent JSON number representations.

Canonical JSON is used for record/hash domains. Signed checkpoint tokens intentionally use deterministic typed `json.Marshal` encoding because typed integer fields must remain standard integer literals for strict decoding.

### Verification and recovery — `verify.go`

Verification is a streaming replay from the metadata-derived anchor. Every record is checked for:

- exact expected sequence;
- expected previous digest;
- valid kind syntax;
- RFC3339Nano timestamp;
- valid canonical payload;
- valid SHA-256 hex encoding;
- recomputed record digest equality.

Repair is deliberately colocated with verification because a repair decision is first an integrity decision. The only mutation repair performs is truncating an incomplete final line after the complete prefix has already verified successfully.

### Query — `query.go`

Querying is implemented as a visitor over the same full verification scan. Filters never bypass integrity validation.

Supported selectors:

- exact record kind;
- record-kind prefix;
- inclusive sequence range;
- inclusive timestamp range;
- output limit.

`Limit` restricts returned records, not verification depth. The scan continues to EOF even after enough matches have been collected.

### Statistics — `stats.go`

Statistics use a full verification scan and derive:

- journal byte size;
- payload byte size;
- verified head sequence/hash;
- first/last record timestamp;
- distinct event kinds;
- count per kind.

No metric is emitted if integrity validation fails.

### Checkpoints — `checkpoint.go`

A plain checkpoint is:

```text
journal_id:sequence:hash
```

It can represent any trusted prefix. Verification of a historical checkpoint succeeds even after the journal grows, as long as the recorded sequence/hash remains in the valid chain.

### Signed checkpoints — `signing.go`

Signed checkpoints authenticate a plain checkpoint with Ed25519.

The generated token format is:

```text
CT1.<base64url(payload-json)>.<base64url(ed25519-signature)>
```

The signature domain is prefixed with:

```text
chaintrail-checkpoint-signature-v1\n
```

The payload includes:

- token version;
- journal ID;
- sequence;
- record hash;
- signing timestamp;
- SHA-256 key ID of the public key.

Private keys are PKCS#8 PEM and public keys are PKIX PEM. Key generation uses `O_EXCL` semantics so an existing key pair cannot be overwritten accidentally.

## Core invariants

### Journal identity is immutable

The metadata file binds the journal ID and creation timestamp into the initial anchor. Modifying metadata changes the anchor and invalidates the complete record chain.

### Sequence is contiguous

For record `N`:

```text
record.seq == N
```

There are no tombstones or sparse sequences.

### Previous hash is exact

For `N > 1`:

```text
record[N].prev == record[N-1].hash
```

For record 1:

```text
record[1].prev == metadata_anchor
```

### Payloads are stored canonically

Verification recomputes the canonical representation of the stored payload and requires byte equality. A payload that parses as equivalent JSON but is stored with a different representation is rejected.

### `head.json` is not authoritative

The cache may accelerate append but must be disposable. Verification/query/stats/tail derive results by replaying the authoritative journal.

### Repair never invents history

Recovery can remove only bytes that do not form a complete final record. It cannot rewrite digests, resequence records, skip bad records, or regenerate earlier history.

## Append path

The normal append path is:

```text
caller
  -> validate kind + payload size
  -> canonicalize payload
  -> load immutable metadata
  -> acquire exclusive journal lock
  -> stat journal
  -> resolve trusted head cache / final record
  -> construct sequence/time/kind/payload/prev
  -> calculate hash
  -> JSON-encode complete record line
  -> append line
  -> fsync(journal.ndjson)
  -> atomically replace head.json
  -> fsync(journal directory)
  -> release lock
```

### Why the cache exists

A full O(n) replay before every append makes write cost grow with journal history. Chaintrail instead uses a fast head-resolution path:

- cache journal byte size must match the authoritative file size;
- cache sequence/hash must match the final record;
- final record digest must verify locally.

If those checks fail, the cache is rebuilt using a complete verification scan.

This is a performance optimization, not proof that every earlier byte is unchanged. A same-size modification to an earlier record is detected by full verification, query, stats, or tail. Operational environments that require a complete verification boundary before sensitive writes should run `verify` and retain an external checkpoint at the relevant control point.

## Verification path

```text
caller
  -> load metadata
  -> acquire shared lock
  -> derive metadata anchor
  -> read journal line-by-line
  -> decode strict record schema
  -> validate seq/prev/kind/time/payload/hash
  -> optionally match checkpoint at target sequence
  -> optionally visit record for query/stats/tail
  -> compare expected current head if configured
  -> return verified journal identity + head
```

Verification is streaming. Memory usage is bounded by the maximum record size plus visitor state.

## Query and statistics path

Both features share the verification engine instead of maintaining secondary indexes.

This avoids two classes of bugs:

1. an index claiming a record exists after the underlying journal was corrupted;
2. an early query result hiding corruption later in the file.

The trade-off is O(n) analysis cost. That is an intentional fit for the project’s local, auditable scope.

## Checkpoint trust model

### Plain checkpoint

A plain checkpoint detects replacement/truncation only if the checkpoint itself is stored somewhere an attacker cannot replace together with the journal.

### Signed checkpoint

A signed checkpoint adds authentication: possession of the expected public key can establish that the checkpoint was signed by the corresponding private key.

It still requires independent custody:

- storing the signed token only inside `.chaintrail/` adds no rollback protection;
- storing the private key beside the journal weakens the authentication boundary;
- a compromised private key allows new forged checkpoints.

The recommended separation is:

```text
journal host              trust domain
------------              ------------
.chaintrail/               private signing key
public key (optional)      signed checkpoint archive
                           ticket/release/backup metadata
```

The public key can safely live on the journal host. The private key should not when stronger independence is required.

## Concurrency model

### Unix-like systems

`syscall.Flock` provides:

- shared locks for read/verification operations;
- exclusive locks for append and repair.

Multiple verified readers may coexist. Writers serialize against other writers and readers.

### Windows

The current build provides an explicit unsupported locking implementation rather than silently performing unlocked mutations. Windows release artifacts are therefore not published.

## Crash consistency

### Initialization

Metadata is written atomically. The journal file is created exclusively and synced. The initial head cache is written only after the journal exists.

### Append

The record is synced before the head cache changes. The important failure windows are:

| Failure point | Result |
| --- | --- |
| Before journal write | No new record. |
| During record write | Potential incomplete final line; `verify` fails, `repair` may remove only that tail. |
| After record fsync, before head update | Record is authoritative; next append can rebuild stale cache. |
| During head replacement | Journal remains authoritative; cache is derivative. |
| After head replacement | Append complete. |

### Repair

Repair verifies the intact prefix before truncation, then truncates, syncs the journal, and refreshes the head cache.

## File permissions

Default modes:

- journal directory: `0750` when Chaintrail creates it;
- metadata/journal/cache/lock: `0640` where explicitly created;
- signing private key: `0600`;
- signing public key: `0644`.

Host policy may make these more restrictive.

## Error model

Library errors fall into two broad groups:

- operational/configuration errors (`error`): invalid input, missing files, unsupported key/platform, I/O failures;
- integrity errors (`*IntegrityError`): journal history cannot be validated.

The CLI maps integrity errors to exit code 3 so automation can distinguish “journal evidence is invalid” from ordinary operational failure.

Signature verification failure is not itself an `IntegrityError`: the token/public-key pair may simply be wrong. Once a token is authenticated, disagreement between its checkpoint and journal history becomes an integrity error.

## Extensibility boundaries

Chaintrail is structured as a reusable Go package plus a thin CLI. Future embedding can call `Open`, `Append`, `Verify`, `Query`, `Stats`, and checkpoint APIs directly.

The project deliberately avoids extension points that would weaken determinism:

- no user-defined hashing plugins;
- no network `$include`-style payload resolution;
- no background mutation service;
- no automatic corruption rewriting.

## Related documents

- [On-disk format](FORMAT.md)
- [Security and threat model](SECURITY.md)
- [Operations runbook](OPERATIONS.md)
- [Testing strategy](TESTING.md)
- [Architecture decision records](adr/)
