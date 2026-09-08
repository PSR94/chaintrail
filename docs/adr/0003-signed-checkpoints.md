# ADR 0003: Ed25519-signed external checkpoints

- Status: Accepted
- Date: 2026-09-08

## Context

A hash chain proves internal consistency, but an older valid prefix is still internally consistent. Detecting rollback requires a trusted value outside the journal. Plain checkpoint strings provide that value, but they do not authenticate who created them.

## Decision

Support optional Ed25519 signatures over checkpoint payloads.

The token format is versioned (`CT1`) and signs a deterministic JSON payload containing journal ID, sequence, hash, signing time, and public-key ID. Signatures use an explicit Chaintrail domain prefix.

Key files use standard interoperable encodings:

- private key: Ed25519 PKCS#8 PEM;
- public key: Ed25519 PKIX PEM.

Key generation refuses overwrites and creates private files with mode `0600`.

## Why Ed25519

- available in the Go standard library;
- compact fixed-size keys/signatures;
- no nonce management requirement for callers;
- straightforward verification API;
- broadly understood security properties;
- avoids runtime dependencies and external crypto tooling.

## Consequences

### Positive

- a checkpoint can be authenticated independently of the journal host;
- public keys can be widely distributed without allowing new checkpoint creation;
- signed historical checkpoints remain useful after the journal grows;
- key ID makes operational key rotation/audit easier.

### Negative

- private-key custody becomes an operational responsibility;
- compromise of the signing key allows forged future checkpoints;
- old public keys must be retained while historical tokens remain relevant;
- signatures authenticate checkpoint creation, not individual event authorship.

## Trust boundary

Signed tokens only improve rollback detection when token/private-key custody is independent enough from journal storage. Keeping the journal, private key, and only token copy on the same compromised host defeats that goal.

## Rejected alternatives

### HMAC

A shared secret would make every verifier capable of minting checkpoints. Public-key verification provides cleaner separation between signing and verification roles.

### RSA/ECDSA

Both are viable but add larger keys or more operational/implementation complexity without a benefit needed by this project.

### Per-record signatures

Per-record signatures would change the project’s authorship model, key-distribution problem, storage overhead, and caller API substantially. The current requirement is authenticated external trust anchors, not per-event identity proof.
