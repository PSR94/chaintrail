# Security and threat model

## Security objective

Chaintrail is designed to make unauthorized changes to a local audit history detectable. It does not attempt to prevent a privileged attacker from writing files; instead it creates cryptographic relationships that later verification can check.

The strongest deployment model combines:

1. an append-only local journal;
2. regular full-chain verification;
3. signed checkpoints created with a private key outside the journal directory;
4. checkpoint copies stored in an independent trust domain.

## Assets

The security-relevant assets are:

- journal history (`journal.ndjson`);
- journal identity (`meta.json`);
- trusted external checkpoint tokens;
- Ed25519 signing private key;
- configured public key used by verifiers;
- operational evidence that links a checkpoint to an external event such as a release or backup.

`head.json` is explicitly not a security asset. It is a rebuildable cache.

## Attacker capabilities considered

Chaintrail is designed to detect evidence manipulation when an attacker can do one or more of the following:

- edit bytes in existing records;
- delete records;
- insert records;
- reorder records;
- replace a canonical payload with different content;
- truncate the journal;
- replace the journal directory with an older internally consistent copy;
- alter or delete the head cache;
- append malformed or partial bytes after a valid prefix.

Rollback/replacement detection requires a trusted checkpoint that is not replaced with the journal.

## Capabilities outside the model

Chaintrail cannot provide a meaningful guarantee when an attacker can simultaneously control every trust boundary, for example:

- modify the journal and all external checkpoint copies;
- steal the signing private key and replace trusted public-key configuration;
- modify the Chaintrail executable/process before it hashes or verifies data;
- compromise the kernel/filesystem implementation used for locking and durability;
- alter the source event before Chaintrail receives it;
- suppress events entirely before they are appended.

The project is an integrity primitive, not a trusted-computing-base replacement.

## Security properties

### Modification detection

Changing a stored record changes the recomputed hash. Because the next record contains the previous hash, a modification also breaks the link to the following record unless the attacker rewrites all later records.

Rewriting all later records still changes the externally observable head/checkpoint digest.

### Insertion/deletion/reordering detection

Sequence numbers and exact previous-hash linkage make insertion, deletion, and reordering detectable during replay.

### Canonical payload enforcement

Payload bytes are stored canonically. Verification requires the stored payload to equal its canonical encoding, preventing alternative JSON formatting from creating ambiguous evidence representations.

### Truncation detection

A local chain can validate an older prefix by itself, so truncation is distinguishable from a valid historical state only when an expected head or external checkpoint is available.

### Signed checkpoint authentication

Ed25519 signed checkpoints authenticate a checkpoint payload under the configured public key. The signature covers journal ID, sequence, hash, signing time, token version, and key ID.

The signature does not mean every event was individually signed by an actor. It means the checkpoint was signed by whoever controlled the private key at that time.

## Checkpoint custody

### Weak deployment

```text
journal host:
  .chaintrail/
  signed-checkpoint.txt
  private-key.pem
```

This gives little independent rollback protection. A host compromise can replace all three.

### Better deployment

```text
journal host:
  .chaintrail/
  public-key.pem

release/backup control host:
  private-key.pem
  signed checkpoints
```

The journal host can verify checkpoints but cannot mint new authenticated checkpoints.

### Stronger operational pattern

Create checkpoints at externally meaningful control points and store them in systems with separate access control:

- release records;
- change-management tickets;
- object storage with retention/versioning;
- backup catalogs;
- a separate audit host;
- build/release artifacts.

The independent system does not need to understand Chaintrail internals. It only needs to preserve the token faithfully.

## Key management

### Generation

`chaintrail keygen` creates an Ed25519 pair using `crypto/rand`.

It refuses to overwrite existing key files. If public-key creation fails after private-key creation, Chaintrail removes the newly created private file to avoid leaving a misleading partial pair.

### File modes

Generated private keys use `0600`; public keys use `0644`. Directory creation uses `0750`.

These are defaults, not a substitute for host policy. More restrictive permissions are acceptable.

### Rotation

Key rotation is handled operationally rather than hidden behind automatic behavior:

1. generate a new key pair;
2. retain the old public key for verification of historical tokens;
3. create and store a final checkpoint signed by the retiring key;
4. begin creating new checkpoints with the new key;
5. record the rotation event externally and, if useful, inside the journal.

Do not delete old public keys while historical signed checkpoints still need verification.

### Compromise

If a private key is suspected compromised:

1. stop treating new tokens from that key as trusted;
2. preserve the last checkpoint known to predate compromise;
3. verify the journal against that checkpoint;
4. generate a new key pair in a clean environment;
5. record the trust transition externally;
6. investigate events after the last trusted checkpoint separately.

A compromised private key does not retroactively alter already archived tokens, but it allows an attacker to mint competing tokens for any checkpoint payload they can construct.

## Append fast path and full verification

A normal append does not replay the entire journal on every write. The head fast path checks:

- journal byte size against the cache;
- cache sequence/hash against the final record;
- final record digest validity.

This avoids O(n) write amplification, but a same-size edit to an earlier record may not be noticed until a full-chain operation occurs.

Full-chain operations are:

- `verify`;
- `query`;
- `stats`;
- `tail`;
- repair prefix validation;
- checkpoint creation (because `Checkpoint()` calls `Verify`).

For higher-assurance workflows, make full verification/checkpoint creation part of the control boundary. Examples:

- verify before release approval;
- create a signed checkpoint after deployment completion;
- verify before exporting audit evidence;
- verify before and after restoring from backup.

## Repair safety

`repair` is not a general corruption fixer.

It may truncate only an incomplete final line. It first verifies the complete prefix. If an earlier record is invalid, repair returns an integrity error and leaves the file unchanged.

This prevents automatic recovery from silently erasing evidence of tampering.

## Query safety

Query filters do not reduce verification depth. A limited query cannot return an apparently valid early result when a later journal record is corrupt.

This makes query cost linear in journal length but keeps the security model simple and predictable.

## Stats safety

Statistics are derived only after validated record visits. Chaintrail does not trust `head.json` or a separate metrics index for record counts or head identity.

## Input handling

### Payload size

Input payloads are capped at 1 MiB. Record reads have a bounded maximum above the payload cap to prevent unbounded line allocation.

### Record schema

Stored records reject unknown top-level fields and trailing JSON values.

### Time

Stored record timestamps must parse as RFC3339Nano during verification.

### Kind

Kinds use a constrained ASCII syntax to keep logs and command-line filters predictable.

## Filesystem assumptions

Chaintrail assumes a filesystem that provides conventional semantics for:

- append writes;
- `fsync` on files;
- atomic rename within one directory;
- directory `fsync`;
- Unix advisory `flock` on supported platforms.

Network filesystems and exotic storage layers can provide weaker or different guarantees. Validate those semantics before relying on Chaintrail for an audit control.

## Confidentiality

Chaintrail does not encrypt payloads. Anyone with file read access can read the journal.

Do not write secrets, authentication tokens, private keys, passwords, regulated data, or sensitive payload fields unless filesystem access policy and your data-handling rules explicitly allow it.

Hash chaining is not encryption.

## Authenticity of individual events

The journal records what the caller supplied. It does not authenticate the human/service represented inside a payload.

If per-event actor authenticity is required, the caller should include independently verifiable evidence such as a signed event envelope or an identity assertion from a trusted upstream system. That is outside Chaintrail’s current protocol.

## Availability

Chaintrail provides no replication or quorum. Loss of the journal files is data loss unless backups exist.

Backups should preserve bytes exactly and should be paired with external checkpoints so restoration of an unexpectedly old snapshot is detectable.

## Denial of service

A process with write permission can prevent successful verification by corrupting the journal. This is detectable but still impacts availability.

Likewise, a process holding an advisory lock indefinitely can block other operations. Host-level process supervision is responsible for availability.

## Dependency and supply-chain surface

Runtime code uses the Go standard library only. CI uses official GitHub actions. Tagged release artifacts are built from the repository source in GitHub Actions and accompanied by SHA-256 checksum files.

Consumers requiring stronger supply-chain guarantees should pin release checksums in their own deployment system and verify the repository/tag provenance according to their policy.

## Security reporting

Do not publish a vulnerability report containing private keys, production journal contents, credentials, or other secrets.

For a security issue in this repository, open a GitHub security advisory/private report when that feature is available for the repository. If private reporting is unavailable, disclose only the minimal non-sensitive reproduction needed to establish contact before sharing exploit details.

## Security review checklist

Before production-like use, verify:

- journal directory permissions fit the service identity;
- signing private key custody is independent enough for the threat model;
- public key distribution is trustworthy;
- checkpoint storage is independent of journal storage;
- full verification occurs at meaningful control boundaries;
- backups preserve exact bytes;
- payloads contain no unintended secrets;
- filesystem locking/durability semantics are understood;
- incident procedures preserve corrupted evidence before repair.

## Related documents

- [Architecture](ARCHITECTURE.md)
- [Format](FORMAT.md)
- [Operations](OPERATIONS.md)
- [ADR 0003: signed checkpoints](adr/0003-signed-checkpoints.md)
