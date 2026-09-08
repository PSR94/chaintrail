#!/usr/bin/env bash
set -euo pipefail

CHAINTRAIL_BIN="${CHAINTRAIL_BIN:-chaintrail}"

if (( $# != 3 )); then
  cat >&2 <<'USAGE'
Usage:
  verify-release.sh JOURNAL_DIR CHECKPOINT_FILE PUBLIC_KEY
USAGE
  exit 2
fi

journal_dir="$1"
checkpoint_file="$2"
public_key="$3"

if [[ ! -f "$checkpoint_file" ]]; then
  echo "verify-release: checkpoint file does not exist: $checkpoint_file" >&2
  exit 1
fi

mapfile -t lines < "$checkpoint_file"
if (( ${#lines[@]} != 1 )) || [[ -z "${lines[0]}" ]]; then
  echo "verify-release: checkpoint file must contain exactly one non-empty token line" >&2
  exit 1
fi

token="${lines[0]}"

"$CHAINTRAIL_BIN" verify \
  -dir "$journal_dir" \
  -signed-checkpoint "$token" \
  -public-key "$public_key"
