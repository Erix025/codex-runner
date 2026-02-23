# Error map

- `failed to create ssh forward`: SSH target invalid or authentication blocked.
- `daemon not healthy`: `codexd` process not listening on configured port.
- `machine.ssh or machine.addr is required for check`: machine profile is missing both connectivity paths.
- `cwd not allowed`: provided path violates server whitelist constraints.
- `ref is required when project_id is set`: include `--ref` with `--project`.
