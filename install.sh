#!/usr/bin/env bash
#
# Install the latest issue-viewer + issue-cli release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/michal-franc/ai-workflow-engine/main/install.sh | bash
#
# Env vars:
#   INSTALL_DIR   target directory for binaries (default: $HOME/.local/bin)
#   VERSION       specific tag to install (default: latest)

set -euo pipefail

REPO="michal-franc/ai-workflow-engine"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-}"

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) err "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) err "unsupported arch: $(uname -m)" ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

if [ "$OS" = "windows" ] && [ "$ARCH" = "arm64" ]; then
  err "no prebuilt binary for windows/arm64"
fi

if [ -z "$VERSION" ]; then
  info "resolving latest release from github.com/$REPO"
  # Fetch into a variable first, then parse — piping curl into grep -m1 with
  # `set -o pipefail` would kill curl with SIGPIPE and abort the script.
  JSON="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")" \
    || err "could not fetch latest release metadata"
  VERSION="$(printf '%s\n' "$JSON" | sed -nE 's/.*"tag_name": *"([^"]+)".*/\1/p' | head -n1)"
  [ -n "$VERSION" ] || err "could not parse tag_name from release metadata"
fi

EXT="tar.gz"
if [ "$OS" = "windows" ]; then EXT="zip"; fi

ASSET="issue-viewer_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

info "downloading $ASSET"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL -o "$TMP/$ASSET" "$URL" || err "download failed: $URL"

info "extracting"
cd "$TMP"
if [ "$EXT" = "zip" ]; then
  command -v unzip >/dev/null || err "unzip is required to extract $ASSET"
  unzip -q "$ASSET"
else
  tar -xzf "$ASSET"
fi

STAGE="issue-viewer_${VERSION}_${OS}_${ARCH}"
[ -d "$STAGE" ] || err "expected directory $STAGE not found in archive"

mkdir -p "$INSTALL_DIR"
SUFFIX=""
if [ "$OS" = "windows" ]; then SUFFIX=".exe"; fi

install -m 0755 "$STAGE/issue-viewer$SUFFIX" "$INSTALL_DIR/issue-viewer$SUFFIX"
install -m 0755 "$STAGE/issue-cli$SUFFIX"    "$INSTALL_DIR/issue-cli$SUFFIX"

info "installed issue-viewer $VERSION → $INSTALL_DIR/issue-viewer$SUFFIX"
info "installed issue-cli    $VERSION → $INSTALL_DIR/issue-cli$SUFFIX"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf '\nnote: %s is not on your PATH. Add this to your shell rc:\n  export PATH="%s:$PATH"\n' "$INSTALL_DIR" "$INSTALL_DIR" ;;
esac
