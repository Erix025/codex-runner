#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <machine>" >&2
  exit 2
fi

machine="$1"

echo "# check"
codex-remote machine check --machine "$machine" || true

echo "# smoke submit"
codex-remote exec start --machine "$machine" --cmd "hostname" || true

