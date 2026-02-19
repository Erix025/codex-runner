# `codex-remote` result contract

## `exec result`

- Returns single JSON object.
- Important keys:
  - `status`: `running` or `finished`
  - `exit_code`: present when finished
  - `error`: optional runtime error detail
  - `artifacts`: optional structured artifacts array

## `exec logs`

- Returns NDJSON lines.
- Read line-by-line; do not assume a JSON array.
- Supports `tail_lines`, `since`, `until` filters.

## `exec watch`

- Streams logs and ends with one summary event containing:
  - `exec_id`
  - `status`
  - `exit_code`
  - `duration_ms`
  - `stdout_log_path` / `stderr_log_path`

## Operator policy

- Always preserve `exec_id` in responses.
- Do not fabricate completion; rely on returned status and exit code.
