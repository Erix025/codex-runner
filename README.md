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
./codexd serve --config ~/.codexd/config.yaml
```

Minimal config example: `examples/codexd-config.yaml`.

Notes:
- `codexd` listens on `127.0.0.1:7337` by default (intended to be reached via SSH/VSCode port-forward).
- Config parsing supports **JSON** and a **small YAML subset** (see `internal/shared/miniyaml` limitations).

## Local: `codex-remote`

Config example: `examples/codex-remote-config.yaml`.

### Codex-facing commands (JSON/JSONL output)

```bash
./codex-remote exec start  --machine gpu1 --cmd "nvidia-smi"
./codex-remote exec result --machine gpu1 --id <exec_id>
./codex-remote exec logs   --machine gpu1 --id <exec_id> --stream stdout --tail 2000
./codex-remote exec cancel --machine gpu1 --id <exec_id>
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
