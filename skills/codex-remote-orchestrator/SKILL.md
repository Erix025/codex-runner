---
name: codex-remote-orchestrator
description: End-to-end control skill for codex-remote remote execution. Use when user wants one entrypoint to check machine connectivity, submit commands, fetch status/logs, and troubleshoot failures without manually switching skills.
---

# codex-remote-orchestrator

Run the full remote execution chain through one skill.

## Execution Flow

1. Validate machine readiness.
2. Choose execution mode (fast sync or async).
3. For async: submit command and capture `exec_id`.
4. For async: query result status and fetch logs on demand.
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

## Step 2: Choose Mode

Fast sync mode (short commands):

```bash
codex-remote exec run --machine "$MACHINE" --cmd "$CMD"
```

Async mode (long-running commands):

```bash
codex-remote exec start --machine "$MACHINE" --cmd "$CMD"
```

Classification is owned by the caller; do not auto-detect inside this tool.

## Step 3: Submit Command (Async Only)

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

Stdout:

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stdout --tail 2000
```

Stderr:

```bash
codex-remote exec logs --machine "$MACHINE" --id "$EXEC_ID" --stream stderr --tail 2000
```

Logs are JSONL. Parse line-by-line.

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

