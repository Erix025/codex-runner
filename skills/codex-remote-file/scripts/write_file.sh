#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: $0 <machine> <remote_path> <local_file_or_content> [mode]" >&2
  exit 2
fi

machine="$1"
remote_path="$2"
source="$3"
mode="${4:-0644}"

if [[ -f "$source" ]]; then
  codex-remote file write --machine "$machine" --dst "$remote_path" --src "$source" --mode "$mode" --mkdir
else
  codex-remote file write --machine "$machine" --dst "$remote_path" --content "$source" --mode "$mode" --mkdir
fi
