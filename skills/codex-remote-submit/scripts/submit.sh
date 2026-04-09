#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <machine> <cmd> [project] [ref] [shell]" >&2
  exit 2
fi

machine="$1"
cmd="$2"
project="${3:-}"
ref="${4:-}"
shell="${5:-}"

shell_args=()
if [[ -n "$shell" ]]; then
  shell_args=(--shell "$shell")
fi

if [[ -n "$project" ]]; then
  codex-remote exec start --machine "$machine" --project "$project" --ref "$ref" "${shell_args[@]}" --cmd "$cmd"
else
  codex-remote exec start --machine "$machine" "${shell_args[@]}" --cmd "$cmd"
fi

