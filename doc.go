// Package chaintrail implements a dependency-free, tamper-evident local audit
// journal with deterministic JSON payloads, SHA-256 record chaining, full-chain
// verification, conservative recovery, integrity-preserving queries, verified
// statistics, and optional Ed25519-authenticated external checkpoints.
//
// The package treats meta.json and journal.ndjson as authoritative history.
// head.json is only a derivative append-performance cache and is not accepted as
// proof that earlier records are valid. Call Verify, Query, Stats, or Tail when
// a workflow requires a complete replay of journal integrity.
//
// Chaintrail provides tamper evidence rather than confidentiality or tamper
// prevention. Rollback detection requires a trusted checkpoint stored outside
// the journal's own trust boundary.
package chaintrail
