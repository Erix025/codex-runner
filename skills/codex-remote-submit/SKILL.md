---
name: codex-remote-submit
description: Submit remote command execution through codex-remote and return exec_id for later tracking. Use when user asks to run a command on a configured machine, including optional project/ref context.
---

# codex-remote-submit

Use this skill to start async execution on a remote machine and return a stable `exec_id`.
For fast synchronous streaming, use `codex-remote exec run` via orchestrator routing.

## Inputs To Confirm

- `machine` (required): machine profile name in `~/.config/codex-remote/config.yaml`
- `cmd` (required): shell command string
- `project` (optional): project id configured on remote `codexd`
- `ref` (optional but required if `project` is provided): branch/tag/commit
- `cwd` (optional): working directory
- `env` (optional): repeatable `KEY=VAL`

## Command Pattern

```bash
codex-remote exec start --machine "$MACHINE" --cmd "$CMD"
```

With optional git context:

```bash
codex-remote exec start --machine "$MACHINE" --project "$PROJECT" --ref "$REF" --cmd "$CMD"
```

## Output Contract

- Expect one-line JSON on stdout.
- Extract and return:
  - `exec_id`
  - `machine`
  - `status`
- If command exits non-zero, treat submission as failed and report stderr directly.

## Follow-up Guidance

- For status: use `codex-remote-result`.
- For logs: use `codex-remote-result` log flow.
- For fast sync commands: use `exec run` instead of this skill.
