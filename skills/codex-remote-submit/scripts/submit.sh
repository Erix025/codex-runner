#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <machine> <cmd_or_script> [project] [ref] [shell]" >&2
  exit 2
fi

machine="$1"
cmd_or_script="$2"
project="${3:-}"
ref="${4:-}"
shell="${5:-}"

args=(--machine "$machine")

if [[ -f "$cmd_or_script" ]]; then
  args+=(--script "$cmd_or_script")
else
  args+=(--cmd "$cmd_or_script")
fi

[[ -n "$project" ]] && args+=(--project "$project")
[[ -n "$ref" ]] && args+=(--ref "$ref")
[[ -n "$shell" ]] && args+=(--shell "$shell")

codex-remote exec start "${args[@]}"
