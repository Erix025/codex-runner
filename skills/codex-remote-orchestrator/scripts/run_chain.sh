#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <machine> <cmd> [project] [ref]" >&2
  exit 2
fi

machine="$1"
cmd="$2"
project="${3:-}"
ref="${4:-}"

echo "# machine check"
check="$(codex-remote machine check --machine "$machine")"
echo "$check"

if echo "$check" | rg -q '"daemon_ok"\s*:\s*false' && echo "$check" | rg -q '"ssh_ok"\s*:\s*true'; then
  echo "# machine up"
  codex-remote machine up --machine "$machine"
  codex-remote machine check --machine "$machine"
fi

echo "# submit"
if [[ -n "$project" ]]; then
  codex-remote exec start --machine "$machine" --project "$project" --ref "$ref" --cmd "$cmd"
else
  codex-remote exec start --machine "$machine" --cmd "$cmd"
fi
