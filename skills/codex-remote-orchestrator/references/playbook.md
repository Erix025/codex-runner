# Orchestrator playbook

Use this skill as the single entrypoint for remote execution workflows.

Default behavior:

1. Check machine
2. Start daemon if needed
3. Submit command
4. Return exec_id
5. Poll result only when requested
6. Pull logs only when requested

Decision rules:

- If command is very short (for example `hostname`, `echo`), it is acceptable to query result immediately after submit.
- If long-running, return exec_id first and avoid blocking.
- If status is `finished` and `exit_code != 0`, fetch stderr tail before concluding.

