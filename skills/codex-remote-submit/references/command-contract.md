# `codex-remote exec start` contract (async)

Expected stdout JSON fields:

- `exec_id` (string)
- `machine` (string)
- `status` (string, usually `running`)
- `base_url` (string, debug only)

Failure mode:

- Non-zero exit code means request was not accepted by remote daemon.
- Common operator errors:
  - unknown machine
  - connection reset
  - auth token mismatch

Related sync mode:

- Use `codex-remote exec run` for synchronous JSONL streaming.
- Mode selection is handled by caller policy, not by codex-remote.
