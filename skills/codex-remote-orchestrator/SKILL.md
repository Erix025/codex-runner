---
name: codex-remote-orchestrator
description: End-to-end control skill for codex-remote remote execution. Use when user wants one entrypoint to check machine connectivity, submit commands, fetch status/logs, and troubleshoot failures without manually switching skills.
---

# codex-remote-orchestrator

Run the full remote execution chain through one skill.

## Execution Flow

1. Validate machine readiness.
2. Submit command and capture `exec_id`.
3. Query result status.
4. Watch/log retrieval.
5. Troubleshoot automatically on errors.

## Step 1: Machine Readiness

Run:

```bash
codex-remote machine check --machine "$MACHINE"
```

If `daemon_ok=false`, run:

```bash
codex-remote machine up --machine "$MACHINE"
codex-remote machine check --machine "$MACHINE"
```

Abort only if SSH remains unreachable.

## Step 2: Submit Command

Without project context:

```bash
codex-remote exec start --machine "$MACHINE" --cmd "$CMD"
```

With project/ref:

```bash
codex-remote exec start --machine "$MACHINE" --project "$PROJECT" --ref "$REF" --cmd "$CMD"
```

Always return `exec_id` to caller.

## Step 3: Status Query

```bash
codex-remote exec result --machine "$MACHINE" --id "$EXEC_ID"
```

Interpret:

- `running`: execution still in progress.
- `finished`: read `exit_code`.

## Step 4: Logs Query

Preferred unified watch:

```bash
codex-remote exec watch --machine "$MACHINE" --id "$EXEC_ID" --stream both --poll 1s
```

Or explicit logs query (line-safe):

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stdout --tail-lines 200
```

Stderr:

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stderr --tail-lines 200
```

Logs are JSONL. Parse line-by-line.

Time window filters:

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stdout --tail-lines 500 --since 10m
```

Preflight:

```bash
codex-remote exec doctor --machine "$MACHINE" --json
```

## Step 5: Troubleshooting Triggers

On these errors, perform immediate triage:

- `unknown machine`
- `connection reset by peer`
- `unauthorized`
- `exec_id not found`

Triage commands:

```bash
codex-remote machine check --machine "$MACHINE"
codex-remote exec start --machine "$MACHINE" --cmd "hostname"
```

## Output Contract

When interacting with user or agent caller, keep this structure:

1. `machine` and readiness (`ssh_ok`, `daemon_ok`)
2. `exec_id` (if submitted)
3. `status`
4. `exit_code` (if finished)
5. optional `stdout_tail` / `stderr_tail`
6. next action (`wait`, `done`, or `fix-config`)
