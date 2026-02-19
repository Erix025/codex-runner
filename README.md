# codex-runner

`codexd` (remote daemon) + `codex-remote` (local CLI + dashboard) to let the local Codex app execute commands on a remote GPU server via SSH / port-forwarding.

## Build

```bash
go build ./cmd/codexd
go build ./cmd/codex-remote
```

Cross-platform builds (outputs under `dist/`):

```bash
# Linux (amd64 + arm64)
make build-linux

# macOS (amd64 + arm64)
make build-darwin

# Windows (amd64 + arm64)
make build-windows

# All platforms
make build-all
```

## CI / Release

- On every PR: GitHub Actions runs `go test ./...` and `make build-all`.
- On every push to `main` (including merged PRs): GitHub Actions:
  - computes the next semver tag (`vX.Y.Z`, starting at `v0.1.0`),
  - builds all platform binaries into `dist/`,
  - generates `dist/SHA256SUMS`,
  - creates a GitHub Release and uploads artifacts.

Manual single-target example (Linux amd64):

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/linux-amd64/codexd ./cmd/codexd
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/linux-amd64/codex-remote ./cmd/codex-remote
```

If your environment blocks the default Go build cache location, set:

```bash
export GOCACHE=/tmp/go-cache
export GOMODCACHE=/tmp/go-mod
```

## Remote: `codexd`

Start the daemon on the remote server:

```bash
./codexd serve
```

Minimal config example: `examples/codexd-config.yaml`.

Notes:
- `codexd` listens on `127.0.0.1:7337` by default (intended to be reached via SSH/VSCode port-forward).
- Config parsing supports **JSON** and a **small YAML subset** (see `internal/shared/miniyaml` limitations).
- Default config path is `~/.config/codexd/config.yaml`. If missing, `codexd` creates it automatically on first run.

```bash
./codexd version
./codexd update --check
./codexd update --yes
```

## Local: `codex-remote`

Config example: `examples/codex-remote-config.yaml`.
Default config path is `~/.config/codex-remote/config.yaml`. If missing, `codex-remote` creates it automatically on first run.

### Codex-facing commands (JSON/JSONL output)

```bash
./codex-remote exec run    --machine gpu1 --cmd "hostname"
./codex-remote exec start  --machine gpu1 --cmd "nvidia-smi"
./codex-remote exec result --machine gpu1 --id <exec_id>
./codex-remote exec logs   --machine gpu1 --id <exec_id> --stream stdout --tail-lines 200
./codex-remote exec watch  --machine gpu1 --id <exec_id> --stream both --poll 1s
./codex-remote exec doctor --machine gpu1 --json
./codex-remote exec cancel --machine gpu1 --id <exec_id>
./codex-remote version
./codex-remote update --check
./codex-remote update --yes
```

Recommended execution policy:
- Fast command: use `exec run` (synchronous JSONL event stream).
- Long-running command: use async flow `exec start -> exec result -> exec logs`.
- Classification is caller-controlled (for example Codex skill), not auto-detected by `codex-remote`.

### Native file sync

```bash
./codex-remote sync push --machine gpu1 --src ./local-dir/ --dst ~/remote-dir/ --exclude .git --exclude node_modules
./codex-remote sync pull --machine gpu1 --src ~/remote-dir/ --dst ./local-dir/
```

### Direct tunnel rollout (machine config)

`exec start` can use explicit local tunnel + direct addr mode (`ssh -f -N -L ...`) instead of the legacy in-process auto forward:

```bash
./codex-remote exec start --machine M602 --cmd "hostname"
```

Notes:
- Enable it per machine in config:
  - `use_direct_addr: true`
- `exec start/result/logs/cancel` all use this path for that machine.
- Tunnel telemetry is written to stderr JSON (`machine/local_port/exec_id/tunnel_pid/health_latency/retry_count`).

### Machine checks

```bash
./codex-remote machine check --machine gpu1
./codex-remote machine ls
./codex-remote machine ls --json
./codex-remote machine up    --machine gpu1
```

### Machine SSH (with agent forwarding)

For git operations that require your local `ssh-agent` identity, run a command through SSH with `-A`:

```bash
./codex-remote machine ssh --machine gpu1 --cmd "git ls-remote git@github.com:ORG/REPO.git"
```

Optional TTY:

```bash
./codex-remote machine ssh --machine gpu1 --cmd "bash -lc 'git pull'" --tty
```

### Local dashboard

```bash
./codex-remote dashboard --listen 127.0.0.1:8787
```

Open: http://127.0.0.1:8787
