# Health reading guide

`machine check` JSON fields:

- `name`
- `ssh_ok`
- `daemon_ok`
- `latency_ms`
- `error`
- `checked_at`

Recommended reaction:

- `ssh_ok=false`: fix SSH target/keys/network.
- `daemon_ok=false`: run `machine up`, then re-check.

