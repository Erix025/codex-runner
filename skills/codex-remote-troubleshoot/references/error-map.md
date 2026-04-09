# Error map

- `failed to create ssh forward`: SSH target invalid or authentication blocked.
- `daemon not healthy`: `codexd` process not listening on configured port.
- `machine.ssh or machine.addr is required for check`: machine profile is missing both connectivity paths.
- `cwd not allowed`: provided path violates server whitelist constraints.
- `ref is required when project_id is set`: include `--ref` with `--project`.
- `path not allowed`: file write/read path is outside permitted directories. Check `allowed_cwd_roots` in codexd config.
- `file too large`: file exceeds `max_file_size` limit in codexd config (default 50MB).
- `dst path not allowed`: sync upload destination is outside permitted directories.
- `invalid gzip` / `invalid tar`: corrupted archive during daemon sync upload.
