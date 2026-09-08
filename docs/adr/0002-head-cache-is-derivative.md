# ADR 0002: Treat the head cache as derivative state

- Status: Accepted
- Date: 2026-09-08

## Context

A full journal replay before every append provides strong detection of earlier corruption but makes append cost increase linearly with journal history. A local operational journal should support efficient repeated writes without creating a second authoritative store.

## Decision

Maintain `head.json` as a derivative cache containing the last sequence, head hash, and observed journal byte size.

The append fast path accepts the cache only when:

- its byte size matches `journal.ndjson`;
- its sequence/hash matches the decoded final record;
- the final record digest validates.

If those checks fail, Chaintrail performs a complete verification scan and rebuilds the cache.

Full-chain operations (`verify`, `query`, `stats`, `tail`, checkpoint creation, repair-prefix validation) do not trust `head.json` as integrity evidence.

## Consequences

### Positive

- normal append remains independent of total journal length;
- a missing/stale cache is recoverable;
- the authoritative history remains only metadata + journal;
- crash after journal fsync but before cache update is recoverable.

### Negative

- a same-size modification to an earlier record may not be detected by a subsequent append alone;
- operators requiring a full verification boundary must explicitly run a full-chain operation;
- the distinction between append-head validation and full-history verification must be documented clearly.

## Security interpretation

The cache is an optimization, not a trust anchor. External checkpoints are the mechanism for detecting rollback/replacement relative to a known state. Full verification is the mechanism for validating every stored record.

## Rejected alternative

Always replaying the entire chain before every append was rejected because it causes O(n²) total verification work over a long append sequence and makes the cache unnecessary.

A Merkle-tree/index design could improve asymptotic verification but would add additional authoritative structures and considerably more complexity. It is not justified for the current local-journal scope.
