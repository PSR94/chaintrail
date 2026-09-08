# ADR 0001: Append-only NDJSON with chained digests

- Status: Accepted
- Date: 2026-09-08

## Context

Chaintrail needs a local audit history that is easy to inspect, append, back up, and verify without running a database or service. The history must make modification, insertion, deletion, and reordering detectable.

Alternatives considered included SQLite, a custom binary log, and one-JSON-file-per-event storage.

## Decision

Use one append-only NDJSON file. Each record includes a sequence number, timestamp, event kind, canonical JSON payload, previous digest, and record digest.

Record digests use SHA-256 with explicit domain separation. The first previous digest is derived from immutable journal metadata.

## Consequences

### Positive

- history is streamable and human-inspectable;
- appends require one authoritative file write;
- backups are simple byte copies;
- verification is deterministic and independent of a database engine;
- corruption location maps directly to a sequence/line;
- standard JSON tooling can inspect records after integrity has been established.

### Negative

- full verification is O(n);
- queries are O(n) without a secondary index;
- in-place compaction/retention would break the simple chain model;
- large journals eventually require operational rotation rather than transparent compaction.

## Why not SQLite

SQLite would provide indexing, transactions, and richer queries, but it also introduces page-level storage semantics, a dependency/runtime component, backup considerations, and a less transparent audit representation. Chaintrail’s primary requirement is an auditable integrity primitive rather than arbitrary relational querying.

## Why not a binary format

A binary format could be more compact but would make forensic inspection and interoperability harder without providing enough value for the expected local journal sizes.

## Follow-up rules

Changes to top-level record fields or digest construction require an explicit format-version decision. Readers fail closed on unknown record fields rather than silently ignoring them.
