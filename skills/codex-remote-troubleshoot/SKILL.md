---
name: codex-remote-troubleshoot
description: Diagnose common codex-remote and codexd execution failures. Use when start/result/logs commands fail with reset, timeout, unauthorized, unknown machine, or missing exec_id errors.
---

# codex-remote-troubleshoot

Use this skill for fast triage when command execution fails.

## Triage Order

1. Validate machine profile exists in local config.
2. Run `machine check`.
3. Verify local direct addr vs SSH forwarding path.
4. Retry with a tiny command (`hostname`).
5. Inspect remote daemon logs if available.

## Common Errors And Fixes

- `unknown machine`
  - Fix `~/.config/codex-remote/config.yaml` machine name.
- `connection reset by peer`
  - Usually forwarding/daemon lifecycle issue; check daemon health first.
- `unauthorized`
  - Verify token in local machine profile and remote `auth_token`.
- `exec_id not found`
  - Ensure querying the same `machine` where command was submitted.

## Minimal Verification Command

```bash
codex-remote exec start --machine "$MACHINE" --cmd "hostname"
```

If submission succeeds, use result/logs to complete chain.

