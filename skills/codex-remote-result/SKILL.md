---
name: codex-remote-result
description: Query execution status and logs from codex-remote by exec_id. Use when user asks to check progress, inspect stdout/stderr, read tail logs, or fetch final exit_code.
---

# codex-remote-result

Use this skill after an execution has been submitted and `exec_id` is known.

## Inputs To Confirm

- `machine` (required)
- `exec_id` (required)
- `stream` (optional: `stdout` or `stderr`, default `stdout`)
- `tail-lines` (optional, default `200`)

## Status Query

```bash
codex-remote exec result --machine "$MACHINE" --id "$EXEC_ID"
```

Interpretation:

- `status=running`: command is still executing.
- `status=finished`: check `exit_code`.

## Logs Query

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stdout --tail-lines 200
```

Notes:

- Output is JSONL (`{"type":"log","stream":"...","line":"..."}` per line).
- For stderr, set `--stream stderr`.
- Optional time windows: `--since 10m --until 1m`.

Unified mode:

```bash
codex-remote exec watch --machine "$MACHINE" --id "$EXEC_ID" --stream both --poll 1s
```

## Cancel

```bash
codex-remote exec cancel --machine "$MACHINE" --id "$EXEC_ID"
```

Use only when user explicitly asks to stop the command.
