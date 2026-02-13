# `codex-remote` result contract

## `exec result`

- Returns single JSON object.
- Important keys:
  - `status`: `running` or `finished`
  - `exit_code`: present when finished
  - `error`: optional runtime error detail

## `exec logs`

- Returns NDJSON lines.
- Read line-by-line; do not assume a JSON array.

## Operator policy

- Always preserve `exec_id` in responses.
- Do not fabricate completion; rely on returned status and exit code.

