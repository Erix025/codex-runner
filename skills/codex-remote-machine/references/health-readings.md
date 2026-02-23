# Health reading guide

`machine check` JSON fields:

- `name`
- `ssh_ok`
- `daemon_ok`
- `daemon_addr` (present when direct addr health check is used)
- `latency_ms`
- `error`
- `checked_at`

Recommended reaction:

- `daemon_ok=true`: machine is usable, even if `ssh_ok=false` (addr-only mode).
- `daemon_ok=false` and `ssh_ok=true`: run `machine up`, then re-check.
- `daemon_ok=false` and `ssh_ok=false`: fix direct `addr` / local forwarding first; if SSH mode is expected, also fix SSH.
