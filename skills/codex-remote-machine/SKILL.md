---
name: codex-remote-machine
description: Check and recover machine connectivity for codex-remote. Use when user asks which machine is reachable, wants health checks, or needs to start remote codexd daemon.
---

# codex-remote-machine

Use this skill to verify machine readiness before execution.

## Inputs To Confirm

- `machine` (required)

## Connectivity Check

```bash
codex-remote machine check --machine "$MACHINE"
```

Interpretation:

- `ssh_ok=true` and `daemon_ok=true`: ready for execution.
- `ssh_ok=false`: SSH path is broken or host is unreachable.
- `ssh_ok=true` but `daemon_ok=false`: remote daemon is down or unhealthy.

## Start Remote Daemon

```bash
codex-remote machine up --machine "$MACHINE"
```

Then run check again.

## Dashboard

```bash
codex-remote dashboard --listen 127.0.0.1:8787
```

Use for visual fleet monitoring and quick check/up operations.

