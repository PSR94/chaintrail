#!/usr/bin/env bash
set -u

CHAINTRAIL_BIN="${CHAINTRAIL_BIN:-chaintrail}"
CHAINTRAIL_DIR="${CHAINTRAIL_DIR:-.chaintrail}"

usage() {
  cat >&2 <<'USAGE'
Usage:
  deploy-audit.sh SERVICE RELEASE -- COMMAND [ARG...]

Environment:
  CHAINTRAIL_BIN             Chaintrail executable (default: chaintrail)
  CHAINTRAIL_DIR             Journal directory (default: .chaintrail)
  CHAINTRAIL_SIGN_KEY        Optional Ed25519 private key for success checkpoint
  CHAINTRAIL_CHECKPOINT_OUT  Optional checkpoint output path
USAGE
}

valid_id() {
  [[ "$1" =~ ^[A-Za-z0-9._:][-A-Za-z0-9._:]*$ ]]
}

if (( $# < 4 )) || [[ "$3" != "--" ]]; then
  usage
  exit 2
fi

service="$1"
release="$2"
shift 3

if ! valid_id "$service" || ! valid_id "$release"; then
  echo "deploy-audit: service and release must use only letters, digits, '.', '_', ':', and '-'" >&2
  exit 2
fi

# A deployment is a high-trust control point. Require a complete replay before
# recording a new release attempt instead of relying only on append's head fast path.
if ! "$CHAINTRAIL_BIN" verify -dir "$CHAINTRAIL_DIR" >/dev/null; then
  echo "deploy-audit: journal verification failed; deployment command was not started" >&2
  exit 3
fi

started_payload=$(printf '{"service":"%s","release":"%s","pid":%d}' "$service" "$release" "$$")
if ! "$CHAINTRAIL_BIN" append -dir "$CHAINTRAIL_DIR" -kind deploy.started -data "$started_payload" >/dev/null; then
  echo "deploy-audit: could not record deploy.started; deployment command was not started" >&2
  exit 1
fi

"$@"
status=$?

if (( status == 0 )); then
  completed_payload=$(printf '{"service":"%s","release":"%s","status":0}' "$service" "$release")
  if ! "$CHAINTRAIL_BIN" append -dir "$CHAINTRAIL_DIR" -kind deploy.completed -data "$completed_payload" >/dev/null; then
    echo "deploy-audit: deployment succeeded but deploy.completed could not be recorded" >&2
    exit 1
  fi

  if [[ -n "${CHAINTRAIL_SIGN_KEY:-}" ]]; then
    token=$("$CHAINTRAIL_BIN" checkpoint -dir "$CHAINTRAIL_DIR" -sign-key "$CHAINTRAIL_SIGN_KEY") || {
      echo "deploy-audit: deployment succeeded but signed checkpoint creation failed" >&2
      exit 1
    }
    checkpoint_out="${CHAINTRAIL_CHECKPOINT_OUT:-./chaintrail-${service}-${release}.checkpoint}"
    old_umask=$(umask)
    umask 077
    if ! printf '%s\n' "$token" > "$checkpoint_out"; then
      umask "$old_umask"
      echo "deploy-audit: deployment succeeded but checkpoint could not be written to $checkpoint_out" >&2
      exit 1
    fi
    umask "$old_umask"
    printf 'signed_checkpoint=%s\n' "$checkpoint_out"
  fi
  exit 0
fi

failed_payload=$(printf '{"service":"%s","release":"%s","status":%d}' "$service" "$release" "$status")
if ! "$CHAINTRAIL_BIN" append -dir "$CHAINTRAIL_DIR" -kind deploy.failed -data "$failed_payload" >/dev/null; then
  echo "deploy-audit: deployment failed with status $status and deploy.failed could not be recorded" >&2
fi
exit "$status"
