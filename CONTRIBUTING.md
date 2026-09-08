# Contributing

Chaintrail is small by design, but changes can affect durable audit evidence. Treat storage, hashing, recovery, and signing code as protocol-sensitive code.

## Development requirements

- Go 1.22 or newer.
- A Unix-like environment for locking-dependent integration behavior.
- `make` for the documented quality targets.

No runtime third-party Go modules are required.

## Before changing code

Read the documents relevant to your change:

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/FORMAT.md`](docs/FORMAT.md)
- [`docs/SECURITY.md`](docs/SECURITY.md)
- [`docs/TESTING.md`](docs/TESTING.md)
- [`docs/adr/`](docs/adr/)

If a change modifies a durable format, cryptographic domain, trust boundary, or recovery rule, add/update an ADR before treating the implementation as complete.

## Local workflow

Run the complete check:

```bash
make check
```

For integrity-sensitive changes also run:

```bash
make cover
make bench
```

`make check` must be clean before a change is proposed.

## Code organization

- root package `chaintrail`: reusable journal/integrity library;
- `cmd/chaintrail`: CLI-only parsing, rendering, and exit mapping;
- `docs`: design and operations documentation;
- `examples`: realistic integration examples;
- `.github/workflows`: CI/release automation.

Do not move integrity logic into the CLI simply to make a command easier to implement. Library behavior should remain independently testable.

## Testing expectations

A bug fix should include a regression test that would have failed before the fix.

New behavior should test both success and relevant failure paths. Pay special attention to:

- corrupt files;
- partial writes;
- invalid/ambiguous input;
- key mismatch/tampering;
- concurrent calls;
- preservation of evidence when an operation refuses to proceed.

Do not mock filesystem behavior that the feature specifically depends on unless there is also a real-file integration test.

## Format-sensitive changes

The following require compatibility review:

- `Meta` fields;
- `Record` fields;
- hash domain strings;
- canonical number behavior;
- checkpoint string syntax;
- signed token payload/token prefix;
- key encodings;
- format version acceptance.

Because record decoding rejects unknown top-level fields, adding a record field is not a transparent compatible change.

## Security-sensitive changes

Changes to signing, key parsing, checkpoint verification, repair, or hash validation should answer:

1. What attacker capability changes?
2. Does failure remain fail-closed?
3. Is any evidence mutated before integrity is established?
4. Is key material ever logged or embedded in errors?
5. Does the change preserve domain separation?
6. What happens with old journals/tokens?

Update `docs/SECURITY.md` when the threat model changes.

## Error semantics

Prefer errors that explain the failing operation while preserving error classification.

Journal-history failures that mean stored evidence is invalid should use `IntegrityError` so the CLI can return exit code 3.

Bad CLI usage belongs to the command layer and maps to exit code 2.

Key/token mismatch is an operational/authentication error until a checkpoint is authenticated and compared with journal history.

## Documentation

A production-facing feature is incomplete until documentation covers:

- purpose and expected use;
- failure behavior;
- operational example;
- security/trust implications where applicable;
- tests/quality implications;
- any durable format change.

Keep the README useful as the entry point and move deep detail into `docs/` rather than duplicating large sections across files.

## Commit style

Prefer focused commits with imperative summaries, for example:

```text
Add signed checkpoint verification
Document journal rotation procedure
Refuse repair after earlier corruption
```

Do not commit generated binaries, coverage files, private keys, temporary journals, or local editor artifacts.

## Release notes

Update `CHANGELOG.md` for user-visible behavior or compatibility changes.

Tagged releases are produced by `.github/workflows/release.yml`. The release workflow reruns the source quality gate before publishing archives/checksums.

## Pull-request review checklist

A reviewer should be able to answer yes to all applicable items:

- behavior is necessary for the stated problem;
- architecture remains understandable;
- no hidden authoritative state was introduced;
- integrity failure remains fail-closed;
- evidence is not silently rewritten;
- tests cover important failure paths;
- race-enabled tests pass;
- documentation matches implementation;
- secrets/private keys are absent from the diff;
- format/security changes have explicit design notes.

## Scope discipline

Chaintrail is not intended to become a generic logging server. Proposals for networking, a database, remote agents, dashboards, or plugin frameworks should first demonstrate why the capability belongs inside the integrity primitive rather than in a caller/integration layer.
