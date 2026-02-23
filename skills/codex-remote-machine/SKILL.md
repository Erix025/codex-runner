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

- `daemon_ok=true`: ready for execution (this includes addr-only machines with `ssh_ok=false`).
- `daemon_ok=false` and `ssh_ok=true`: remote daemon is down/unhealthy; can try `machine up`.
- `daemon_ok=false` and `ssh_ok=false`: neither SSH nor direct addr health path is usable.

## Start Remote Daemon

```bash
codex-remote machine up --machine "$MACHINE"
```

Only run `machine up` when SSH is configured/reachable. For addr-only machines, fix local port-forward or `addr` first, then run check again.

## Dashboard

```bash
codex-remote dashboard --listen 127.0.0.1:8787
```

Use for visual fleet monitoring and quick check/up operations.
