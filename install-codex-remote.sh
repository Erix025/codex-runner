#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="codex-remote"
INSTALL_DIR_DEFAULT="$HOME/bin"
REPO_DEFAULT="Erix025/codex-runner"
VERSION_DEFAULT=""
NO_VERIFY_DEFAULT="0"
NO_OVERWRITE_DEFAULT="0"
QUIET_DEFAULT="0"

INSTALL_DIR="${INSTALL_DIR:-$INSTALL_DIR_DEFAULT}"
REPO="${REPO:-$REPO_DEFAULT}"
VERSION="${VERSION:-$VERSION_DEFAULT}"
NO_VERIFY="${NO_VERIFY:-$NO_VERIFY_DEFAULT}"
NO_OVERWRITE="${NO_OVERWRITE:-$NO_OVERWRITE_DEFAULT}"
QUIET="${QUIET:-$QUIET_DEFAULT}"

log() { if [[ "${QUIET}" != "1" ]]; then echo "$@"; fi; }
err() { echo "Error: $*" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || { err "Missing required command: $1"; return 1; }; }

usage() {
  cat <<USAGE
Usage: $(basename "$0") [options]
  --dir <path>          Install directory (default: $INSTALL_DIR_DEFAULT or $INSTALL_DIR)
  --version <vX.Y.Z>    Install a specific version tag (default: latest)
  --repo <owner/repo>   GitHub repo (default: $REPO_DEFAULT or $REPO)
  --no-verify           Skip checksum verification
  --no-overwrite        Fail if target exists
  --quiet               Reduce output
  -h, --help            Show this help and exit
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) INSTALL_DIR="$2"; shift 2;;
    --version) VERSION="$2"; shift 2;;
    --repo) REPO="$2"; shift 2;;
    --no-verify) NO_VERIFY="1"; shift;;
    --no-overwrite) NO_OVERWRITE="1"; shift;;
    --quiet) QUIET="1"; shift;;
    -h|--help) usage; exit 0;;
    *) err "Unknown option: $1"; usage; exit 2;;
  esac
done

need curl

uname_s=$(uname -s)
uname_m=$(uname -m)
case "$uname_s" in
  Darwin) GOOS="darwin" ;;
  Linux)  GOOS="linux"  ;;
  *) err "Unsupported OS: $uname_s"; exit 1;;
esac
case "$uname_m" in
  x86_64|amd64) GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *) err "Unsupported ARCH: $uname_m"; exit 1;;
esac

ASSET="${BINARY_NAME}-${GOOS}-${GOARCH}"
if [[ -n "$VERSION" ]]; then
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
else
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
fi
BIN_URL="${BASE_URL}/${ASSET}"
SUMS_URL="${BASE_URL}/SHA256SUMS"

TMPDIR=$(mktemp -d 2>/dev/null || mktemp -d -t codex-remote-install)
trap "rm -rf \"$TMPDIR\"" EXIT
BIN_PATH="${TMPDIR}/${ASSET}"
SUMS_PATH="${TMPDIR}/SHA256SUMS"

log "Installing ${BINARY_NAME} for ${GOOS}-${GOARCH} from ${REPO}..."
log "Download: ${BIN_URL}"

curl -fL --retry 3 -o "$BIN_PATH" "$BIN_URL"
chmod +x "$BIN_PATH" || true

verify_checksum() {
  if [[ "$NO_VERIFY" == "1" ]]; then log "Skipping checksum verification (--no-verify)."; return 0; fi
  if command -v sha256sum >/dev/null 2>&1; then HAVE_SHA=sha256sum; elif command -v shasum >/dev/null 2>&1; then HAVE_SHA="shasum -a 256"; else log "No sha256 tool found (sha256sum or shasum). Proceeding without verification."; return 0; fi
  log "Downloading checksums: ${SUMS_URL}"
  curl -fL --retry 3 -o "$SUMS_PATH" "$SUMS_URL"
  expected_path="./${GOOS}-${GOARCH}/${BINARY_NAME}"
  expected=$(grep -iE "[[:space:]]${expected_path}$" "$SUMS_PATH" | head -n1 | awk "{print tolower(\$1)}")
  if [[ -z "$expected" ]]; then err "Expected checksum entry not found for $expected_path"; exit 1; fi
  actual=$($HAVE_SHA "$BIN_PATH" | awk "{print tolower(\$1)}")
  if [[ "$actual" != "$expected" ]]; then err "Checksum mismatch for ${ASSET}: expected $expected got $actual"; exit 1; fi
  log "Checksum verified."
}

verify_checksum

mkdir -p "$INSTALL_DIR"
TARGET="${INSTALL_DIR}/${BINARY_NAME}"
if [[ -e "$TARGET" && "$NO_OVERWRITE" == "1" ]]; then err "Target exists: $TARGET (remove it or omit --no-overwrite)"; exit 1; fi
mv -f "$BIN_PATH" "$TARGET"
chmod +x "$TARGET" || true

log "Installed to: $TARGET"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    log "Note: $INSTALL_DIR is not on your PATH. Add one of the following:"
    if [[ "$GOOS" == "darwin" ]]; then
      log "  echo \"export PATH=\\\"$HOME/bin:$PATH\\\"\" >> ~/.zshrc && source ~/.zshrc"
    else
      log "  echo \"export PATH=\\\"$HOME/bin:$PATH\\\"\" >> ~/.bashrc && source ~/.bashrc"
    fi
    ;;
  esac

if [[ "$QUIET" != "1" ]]; then "$TARGET" version || { err "Installed binary failed to run version"; exit 1; }; fi

log "Done."
