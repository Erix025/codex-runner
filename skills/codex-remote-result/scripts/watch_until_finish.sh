#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <machine> <exec_id>" >&2
  exit 2
fi

machine="$1"
exec_id="$2"

while true; do
  out="$(codex-remote exec result --machine "$machine" --id "$exec_id")"
  echo "$out"
  if echo "$out" | rg -q '"status"\s*:\s*"finished"'; then
    break
  fi
  sleep 2
done

