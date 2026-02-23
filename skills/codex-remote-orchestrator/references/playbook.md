# Orchestrator playbook

Use this skill as the single entrypoint for remote execution workflows.

Default behavior:

1. Check machine
2. Start daemon if needed
3. Choose execution mode
4. For fast command: run sync stream and return final status
5. For async command: submit and return exec_id
6. For async command: poll result only when requested
7. For async command: pull logs only when requested

Decision rules:

- If command is very short (for example `hostname`, `echo`), prefer `exec run`.
- If long-running, use async and return exec_id first.
- Mode decision is caller-owned; do not attempt automatic classification inside codex-remote.
- If status is `finished` and `exit_code != 0`, fetch stderr tail before concluding.
- In readiness checks, use `daemon_ok` as gate; `ssh_ok=false` can still be valid for addr-only machines.
