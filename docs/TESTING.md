# Testing strategy

Chaintrail handles integrity-sensitive state, so tests focus on invariants and failure modes rather than only happy-path command output.

## Quality gate

The repository-level quality gate is:

```bash
make check
```

It runs:

```text
format check -> go vet -> shuffled tests -> race-enabled tests -> build
```

Additional observability targets:

```bash
make cover
make bench
```

## Test layers

### Canonical encoding tests

These verify that semantically equivalent JSON representations produce the same canonical bytes and that stored payloads cannot use a non-canonical representation during verification.

Important cases include:

- object key ordering;
- insignificant whitespace;
- numerically equivalent decimal/exponent forms;
- nested arrays/objects;
- invalid JSON/trailing values.

### Journal lifecycle tests

Coverage includes:

- initialization;
- append sequence/head linkage;
- checkpoint creation;
- historical checkpoint verification after additional appends;
- tail behavior;
- stale head-cache rebuilding.

### Integrity failure tests

Tests intentionally mutate authoritative files and expect `IntegrityError`:

- payload modification;
- invalid stored hash;
- incomplete final record;
- earlier corruption before a partial tail;
- query/statistics over a tampered journal.

A key property is that convenience operations do not accidentally bypass integrity checks.

### Concurrency tests

The concurrent append test launches multiple goroutines writing to the same journal and then verifies:

- all appends completed;
- sequence count equals the number of successful writes;
- the resulting complete chain verifies under the race detector.

This test exercises both Go memory concurrency and OS-level serialization behavior on CI’s Linux runner.

### Recovery tests

Repair tests distinguish recoverable interruption from evidence corruption.

Recoverable:

```text
valid complete prefix + incomplete final bytes
```

Not recoverable automatically:

```text
corrupt complete record + incomplete final bytes
```

The refusal test also verifies that the journal bytes remain unchanged when repair rejects earlier corruption.

### Query tests

Query tests cover:

- exact kind filtering;
- kind-prefix filtering;
- sequence windows;
- inclusive time windows;
- limits;
- validation of contradictory options;
- continued full verification after a limit has already been satisfied.

The last case prevents a subtle failure where a valid early match could hide later corruption.

### Statistics tests

Statistics tests verify counts, kind cardinality, byte fields, record time range, and rejection of tampered journals.

### Signed checkpoint tests

Security-sensitive tests cover:

- key-pair creation;
- private-key mode;
- deterministic token round trip;
- signed historical checkpoint verification after journal growth;
- modified signature rejection;
- wrong-public-key rejection;
- key generation refusing to overwrite an existing private key.

### CLI integration tests

CLI tests invoke the same `run` function used by `main` and exercise real temporary journal directories.

The expanded workflow covers:

```text
init
  -> append
  -> query
  -> stats
  -> keygen
  -> signed checkpoint
  -> append more data
  -> verify against signed historical checkpoint
```

Tests also assert usage exit codes for invalid argument combinations.

## Race detector

CI runs:

```bash
go test -race -timeout=60s ./...
```

This is required because Chaintrail deliberately supports concurrent callers and its tests include concurrent appends and a test-controlled clock protected by a mutex.

A race failure is a release blocker.

## Shuffling

The ordinary test job uses:

```bash
go test -shuffle=on ./...
```

Tests should not depend on package test execution order or persistent mutable state from another case.

## Temporary files

Tests use `testing.T.TempDir()` / `testing.B.TempDir()` and real files rather than an in-memory filesystem. This is intentional because Chaintrail behavior depends on:

- append semantics;
- file size;
- rename;
- fsync paths;
- permissions;
- locking.

Tests should still avoid asserting filesystem behavior that is irrelevant to the documented contract.

## Coverage

`make cover` produces `coverage.out` and prints function-level coverage:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

The project does not currently enforce a single numeric threshold. Integrity-sensitive branches should be tested according to risk; chasing a percentage can encourage low-value tests that do not validate failure semantics.

Coverage output is ignored by Git.

## Benchmarks

Benchmarks are included for:

- canonical JSON processing;
- full verification of a 1,000-record journal.

Run:

```bash
make bench
```

CI executes them with a short benchmark duration as a compile/execution smoke check. They are not hard performance gates because shared CI runners are unsuitable for stable latency thresholds.

For performance investigation, use a controlled machine and record:

- Go version;
- OS/filesystem;
- CPU architecture;
- journal record count;
- typical payload size;
- storage medium;
- benchmark flags.

## Compatibility matrix

CI tests the supported Go floor and the primary development toolchain independently.

The module declares Go 1.22. CI verifies Go 1.22.x and Go 1.23.x builds/tests, while the quality/race job runs on Go 1.23.x.

When raising the minimum Go version:

1. update `go.mod`;
2. update CI matrix;
3. update README badge/documentation;
4. record the compatibility change in `CHANGELOG.md`.

## Release verification

Tagged releases do not skip source verification. The release workflow runs `make check` before producing cross-compiled archives.

Release archives are created for:

- Linux amd64;
- Linux arm64;
- macOS amd64;
- macOS arm64.

A `SHA256SUMS` file accompanies the archives.

Windows release artifacts are intentionally excluded while locking-dependent operations are unsupported there.

## Adding a regression test

A good integrity regression test should:

1. construct the smallest valid journal demonstrating the original condition;
2. apply one controlled mutation or failure condition;
3. assert the precise behavior class (success, operational error, integrity error);
4. assert that recovery does not mutate evidence when it should refuse;
5. avoid relying on wall-clock time or global state.

## Review expectations

Changes to these areas require especially careful tests:

- canonical JSON encoding;
- hash-domain strings;
- record fields;
- metadata fields;
- lock semantics;
- repair/truncation;
- checkpoint parsing;
- signed-token encoding;
- key parsing/generation;
- exit-code mapping.

A format/security change without a regression test and ADR/changelog update should not be merged.
