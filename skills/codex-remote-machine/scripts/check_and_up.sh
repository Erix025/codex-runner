#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <machine>" >&2
  exit 2
fi

machine="$1"
check="$(codex-remote machine check --machine "$machine")"
echo "$check"

if echo "$check" | rg -q '"daemon_ok"\s*:\s*false'; then
  codex-remote machine up --machine "$machine"
  codex-remote machine check --machine "$machine"
fi

