---
name: codex-remote-file
description: Read and write files on remote machines via codexd HTTP API. Use when user needs to create, modify, or read files on a remote machine, especially addr-only machines without SSH.
---

# codex-remote-file

Use this skill to transfer file content to/from remote machines through the daemon HTTP channel, without requiring SSH.

## Inputs To Confirm

- `machine` (required): machine profile name
- `path` (required): absolute path on remote machine
- For write: `content` (inline string) or `src` (local file path)
- For read: `dst` (optional local destination file)

## Write File

```bash
codex-remote file write --machine "$MACHINE" --dst "$REMOTE_PATH" --content "$CONTENT"
```

From local file:

```bash
codex-remote file write --machine "$MACHINE" --dst "$REMOTE_PATH" --src "$LOCAL_FILE" --mkdir
```

With executable permission:

```bash
codex-remote file write --machine "$MACHINE" --dst "$REMOTE_PATH" --src "$LOCAL_FILE" --mode 0755
```

## Read File

```bash
codex-remote file read --machine "$MACHINE" --path "$REMOTE_PATH"
```

Save to local file:

```bash
codex-remote file read --machine "$MACHINE" --path "$REMOTE_PATH" --dst "$LOCAL_FILE"
```

## Output Contract

Write response:
```json
{"ok": true, "path": "/abs/path", "bytes_written": 1234}
```

Read response (without --dst):
```json
{"ok": true, "path": "/abs/path", "content": "<base64>", "size": 1234}
```

## Notes

- Content is transferred as base64 over JSON.
- Max file size is configurable in codexd config (default 50MB).
- Path must be within allowed roots (home dir, data dir, /tmp, or configured allowed_cwd_roots).
- Use `--mkdir` to auto-create parent directories.
