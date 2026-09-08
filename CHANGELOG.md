# Changelog

All notable user-facing changes are recorded here.

The project follows semantic-versioning intent for tagged releases. Until a stable `v1.0.0` contract is declared, minor versions may contain deliberate format/API changes that are documented explicitly.

## Unreleased

### Added

- integrity-preserving `query` command and library API with kind, sequence, time, and limit filters;
- verified `stats` command/API with journal and event-distribution metrics;
- Ed25519 key generation and authenticated signed checkpoint tokens;
- signed historical-checkpoint verification using a configured public key;
- key overwrite protection and private-key permission handling;
- benchmark coverage for canonical JSON and 1,000-record verification;
- Makefile quality targets;
- Go 1.22/1.23 CI compatibility matrix, race detector, coverage, and benchmark smoke checks;
- tagged release workflow for Linux/macOS amd64/arm64 archives with SHA-256 checksums;
- architecture, storage-format, security, operations, testing, ADR, contribution, and deployment-example documentation.

### Changed

- README expanded into a full project entry point with end-to-end operational workflows and repository map;
- GitHub Actions upgraded to current Node-24-compatible official action majors;
- architecture documentation now distinguishes the append head-cache fast path from full-chain verification paths.

## 0.1.0 — 2026-09-08

### Added

- append-only NDJSON journal;
- SHA-256 record chaining anchored to immutable journal metadata;
- deterministic payload canonicalization;
- `init`, `append`, `verify`, `checkpoint`, `tail`, and conservative `repair` commands;
- stale head-cache rebuilding;
- Unix advisory file locking;
- historical checkpoint verification;
- concurrent append tests, tamper tests, repair tests, and CLI smoke tests;
- GitHub Actions CI;
- project banner and architecture diagram.
