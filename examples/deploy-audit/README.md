# Deployment audit example

This example shows how a release script can use Chaintrail as an integrity journal without embedding deployment logic inside Chaintrail itself.

The wrapper records:

```text
deploy.started
  -> execute the supplied deployment command
  -> deploy.completed OR deploy.failed
  -> optional signed checkpoint on success
```

## Build Chaintrail

From the repository root:

```bash
make build
export CHAINTRAIL_BIN="$PWD/bin/chaintrail"
```

## Initialize an audit journal

```bash
"$CHAINTRAIL_BIN" init -dir /tmp/deploy-audit
export CHAINTRAIL_DIR=/tmp/deploy-audit
```

## Run a deployment through the wrapper

```bash
./examples/deploy-audit/deploy-audit.sh \
  api \
  2026.09.08.3 \
  -- \
  sh -c 'echo deploying-api'
```

The example accepts only simple service/release identifiers (`A-Z`, `a-z`, digits, `.`, `_`, `:`, `-`) so it can construct small JSON payloads without an additional JSON dependency.

## Add signed release checkpoints

Generate a key pair outside the journal directory:

```bash
"$CHAINTRAIL_BIN" keygen \
  -private /tmp/chaintrail-keys/release.key.pem \
  -public /tmp/chaintrail-keys/release.pub.pem
```

Then set:

```bash
export CHAINTRAIL_SIGN_KEY=/tmp/chaintrail-keys/release.key.pem
export CHAINTRAIL_CHECKPOINT_OUT=/tmp/release-2026.09.08.3.checkpoint
```

Run the wrapper again. On a successful deployment it writes the signed `CT1...` token to the checkpoint output path with mode `0600`.

The checkpoint file should be moved/copied into the independent release/change record rather than left only on the deployment host.

## Verify a release checkpoint

```bash
./examples/deploy-audit/verify-release.sh \
  /tmp/deploy-audit \
  /tmp/release-2026.09.08.3.checkpoint \
  /tmp/chaintrail-keys/release.pub.pem
```

This verifies:

1. the checkpoint signature under the supplied public key;
2. the checkpoint journal ID/sequence/hash against the actual journal;
3. every record through the current journal head.

## Inspect release history

```bash
"$CHAINTRAIL_BIN" query \
  -dir /tmp/deploy-audit \
  -kind-prefix deploy. \
  -format json

"$CHAINTRAIL_BIN" stats -dir /tmp/deploy-audit
```

## Failure behavior

When the wrapped command exits non-zero, the script appends `deploy.failed` with the exit status and returns the same status. It does not create a signed success checkpoint.

If Chaintrail itself cannot append the failure record, the wrapper reports that secondary failure to stderr while preserving the original deployment exit status.

## Production adaptation

A real release system should additionally decide:

- how the service/release identity is sourced from trusted pipeline metadata;
- where the journal directory lives and which OS identity owns it;
- where the signing private key is held;
- how signed checkpoints are archived independently;
- whether a full `verify` is required before the deployment starts;
- what external release ID/ticket should be included in payloads;
- how journal backups/rotation are handled.

See [`docs/OPERATIONS.md`](../../docs/OPERATIONS.md) and [`docs/SECURITY.md`](../../docs/SECURITY.md).
