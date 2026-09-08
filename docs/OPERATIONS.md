# Operations runbook

This runbook describes a production-like way to operate Chaintrail as a local audit control. Adapt paths, service identities, backup locations, and checkpoint custody to your environment.

## Recommended deployment shape

```text
application/service host
├── /usr/local/bin/chaintrail
└── /var/lib/example-audit/
    ├── meta.json
    ├── journal.ndjson
    ├── head.json
    └── journal.lock

independent trust location
├── chaintrail-checkpoint.key.pem
├── chaintrail-checkpoint.pub.pem
└── archived signed checkpoints
```

The journal and signing private key should not share the same trust boundary when rollback/authentication matters.

## Installation

Build from a reviewed source revision:

```bash
go build -trimpath -o chaintrail ./cmd/chaintrail
```

Or use a tagged GitHub release archive and verify its checksum against `SHA256SUMS` before installation.

Place the binary somewhere controlled by your deployment process, for example:

```text
/usr/local/bin/chaintrail
```

## Service account and permissions

Prefer a dedicated service identity for automated writers.

Example ownership model:

```text
/var/lib/example-audit/    service-user:audit-group  0750
journal files             service-user:audit-group  0640
```

The exact policy is environment-specific. The important separation is that unrelated application users should not receive write access to the journal.

Do not place the Ed25519 private checkpoint key in the journal directory merely for convenience.

## Initialize once

```bash
chaintrail init -dir /var/lib/example-audit
```

Initialization is exclusive and refuses to replace an existing journal.

Record the generated journal ID in the service/change record if journal identity matters operationally.

## Writing events

Prefer small structured payloads with fields useful during incident review.

Example:

```bash
chaintrail append \
  -dir /var/lib/example-audit \
  -kind deploy.started \
  -data '{"service":"api","release":"2026.09.08.3","actor":"release-bot","environment":"prod"}'
```

Use event kinds consistently. A practical convention is:

```text
<domain>.<state-or-action>
```

Examples:

- `deploy.started`
- `deploy.completed`
- `deploy.failed`
- `backup.started`
- `backup.completed`
- `config.applied`
- `maintenance.started`

Avoid putting secrets into payloads. The journal is not encrypted.

## Full verification schedule

A normal append uses a head fast path. Establish explicit full-verification boundaries appropriate to the system.

Suggested boundaries:

- before a release is approved;
- after a deployment completes;
- after a backup completes;
- before exporting records for an audit/incident;
- after restoring a journal backup;
- on a periodic schedule such as hourly/daily depending on journal value and size.

Command:

```bash
chaintrail verify -dir /var/lib/example-audit
```

Treat exit code 3 as an integrity incident, not a transient retry condition.

## Checkpoint workflow

### Generate signing key

Generate the key in an independent trust location:

```bash
chaintrail keygen \
  -private /secure/chaintrail/checkpoint.key.pem \
  -public /secure/chaintrail/checkpoint.pub.pem
```

Copy the public key to verifier hosts as needed. Keep the private key under stronger control.

### Create signed checkpoint

After a meaningful control point:

```bash
TOKEN="$(
  chaintrail checkpoint \
    -dir /var/lib/example-audit \
    -sign-key /secure/chaintrail/checkpoint.key.pem
)"
```

Store the token outside the journal host or at least outside the journal filesystem/access domain.

Recommended metadata to store beside the token:

- journal ID;
- service/environment;
- external operation identifier (release, backup, ticket);
- creation time;
- public-key/key-ID reference.

### Verify against signed checkpoint

```bash
chaintrail verify \
  -dir /var/lib/example-audit \
  -signed-checkpoint "$TOKEN" \
  -public-key /etc/chaintrail/checkpoint.pub.pem
```

The signed checkpoint can represent an older sequence. The journal is allowed to have grown.

## Querying during operations

Queries validate the entire chain before returning data.

Deployment history:

```bash
chaintrail query \
  -dir /var/lib/example-audit \
  -kind-prefix deploy. \
  -format json
```

Time-bounded incident window:

```bash
chaintrail query \
  -dir /var/lib/example-audit \
  -since 2026-09-08T14:00:00Z \
  -until 2026-09-08T16:00:00Z
```

Sequence window:

```bash
chaintrail query -dir /var/lib/example-audit -from 1200 -to 1250
```

Because verification continues after a query limit is satisfied, query latency remains proportional to full journal length.

## Operational statistics

```bash
chaintrail stats -dir /var/lib/example-audit
```

Use statistics for:

- confirming expected event volume;
- monitoring journal growth;
- checking distribution of event kinds;
- recording verified head values in operational diagnostics.

JSON mode:

```bash
chaintrail stats -dir /var/lib/example-audit -format json
```

Do not use `head.json` directly as a trusted metric source.

## Backup

### What to back up

Preserve exact bytes for:

- `meta.json`;
- `journal.ndjson`.

You may also back up `head.json`, but it is not required and can be rebuilt.

The lock file has no historical value.

### Backup procedure

The safest simple procedure is:

1. acquire an operational quiet period or use a filesystem snapshot mechanism with consistent file semantics;
2. run `chaintrail verify`;
3. create a signed checkpoint;
4. copy/snapshot the journal files without reformatting them;
5. store the checkpoint in the backup catalog independently of the backup contents;
6. verify the copied backup in a temporary location if practical.

### Restore procedure

1. restore `meta.json` and `journal.ndjson` exactly;
2. do not trust a restored `head.json`; it may be omitted;
3. run full `verify` against the expected signed/plain checkpoint;
4. run `stats` and compare journal ID/head information with operational records;
5. only then resume writers.

An older but internally valid backup can verify by itself. The external checkpoint is what tells you whether that backup is unexpectedly stale.

## Monitoring

Chaintrail is a CLI/library rather than a daemon, so monitoring is built around command results and journal growth.

Useful signals:

- periodic `verify` exit status;
- `stats` `head_seq` progression;
- journal byte growth;
- expected event-kind counts;
- time since last signed checkpoint;
- writer failures returned by the calling application;
- unexpected repair events.

Avoid automated repair on every verification failure. Integrity failure should first create an incident signal.

## Incident: verification fails

When `verify` returns exit code 3:

1. stop non-essential writers if doing so is operationally safe;
2. preserve a byte-for-byte copy of the journal directory before changing anything;
3. collect the most recent trusted plain/signed checkpoint from the independent store;
4. record filesystem metadata, host time, and relevant process/service state;
5. rerun verification and capture the exact error;
6. determine whether the failure is an incomplete tail or earlier corruption;
7. do not rewrite records or regenerate hashes to make verification pass;
8. compare against backups and trusted checkpoints;
9. only use `repair` when the failure is confirmed to be an incomplete final line and earlier history verifies.

A bad hash, sequence mismatch, or previous-hash mismatch is evidence to investigate.

## Incident: incomplete final line

Symptoms:

```text
journal has an incomplete final record at sequence N
```

Procedure:

1. preserve a copy of the original file;
2. confirm no writer is actively using the journal;
3. run `chaintrail repair`;
4. Chaintrail validates the intact prefix before truncation;
5. run `chaintrail verify` again;
6. create a fresh signed checkpoint after recovery;
7. record the recovery event in your external incident/change record.

`repair` will refuse the operation if earlier history is corrupt.

## Incident: signing key compromise

1. stop issuing checkpoints with the affected key;
2. retain old public key material for historical token verification;
3. identify the last checkpoint known to predate compromise;
4. verify journal history against that checkpoint;
5. generate a new key pair in a clean trust environment;
6. distribute the new public key through a trusted path;
7. create a new checkpoint only after reviewing the transition period;
8. document the key-ID change externally.

## Key rotation without compromise

Plan rotation so both old and new public keys remain available during the retention period.

Recommended handoff:

```text
old key -> final signed checkpoint -> external rotation record -> new key -> first new signed checkpoint
```

Do not silently replace the public key file and discard the previous one if old tokens remain audit evidence.

## Journal growth and retention

Chaintrail does not rewrite or compact journals because those operations conflict with a simple append-only integrity model.

For long-running environments, choose an operational rotation boundary such as month/quarter/release train:

1. full-verify the current journal;
2. create and archive a signed final checkpoint;
3. back up/archive the closed journal directory;
4. initialize a new journal directory;
5. record the predecessor journal ID/final signed checkpoint in your external catalog or first new-journal event.

This is an operational rotation, not an in-place truncation.

## Filesystem considerations

Validate behavior before using:

- NFS/SMB/network filesystem locking;
- container overlay filesystems;
- object-backed FUSE mounts;
- storage that does not provide normal file/directory fsync semantics.

Chaintrail assumes conventional POSIX-like local filesystem behavior on its supported mutation path.

## Containers

If running inside a container:

- mount the journal directory on durable storage;
- ensure only intended containers/users can write the volume;
- do not bake private signing keys into the image;
- verify the storage driver’s durability semantics;
- preserve journal identity across container recreation.

## Automation example

See [`examples/deploy-audit`](../examples/deploy-audit/) for a shell integration that records deployment state, emits a signed checkpoint, and verifies the journal.

## Change management

Integrity-sensitive changes should be treated like storage/protocol changes even when the code diff looks small.

Before deploying a new Chaintrail build:

1. review `CHANGELOG.md`;
2. run the full quality gate;
3. verify existing journals with the old binary;
4. test the new binary against copied representative journals;
5. confirm signed-checkpoint compatibility;
6. deploy gradually where operational impact warrants it.

## Related documents

- [Architecture](ARCHITECTURE.md)
- [Format](FORMAT.md)
- [Security](SECURITY.md)
- [Testing](TESTING.md)
