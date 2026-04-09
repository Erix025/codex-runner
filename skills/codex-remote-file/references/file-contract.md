# File API contract

## `POST /v1/file/write`

Request:
```json
{
  "path": "/abs/path",
  "content": "<base64-encoded>",
  "mode": 420,
  "mkdir_p": true
}
```

Response:
```json
{"ok": true, "path": "/abs/path", "bytes_written": 1234}
```

Errors:
- 400: missing path, non-absolute path, invalid base64
- 403: path not allowed
- 413: file too large
- 500: write failure

## `POST /v1/file/read`

Request:
```json
{"path": "/abs/path"}
```

Response:
```json
{"ok": true, "path": "/abs/path", "content": "<base64>", "size": 1234}
```

Errors:
- 400: missing path, non-absolute path, path is directory
- 403: path not allowed
- 404: file not found
- 413: file too large
- 500: read failure
