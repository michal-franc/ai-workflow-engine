#!/usr/bin/env bash
#
# Install the latest issue-viewer + issue-cli release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/michal-franc/ai-workflow-engine/main/install.sh | bash
#
# Env vars:
#   INSTALL_DIR   target directory for binaries (default: $HOME/.local/bin)
#   VERSION       specific tag to install (default: latest), must match vX.Y.Z[...]
#   FORCE         set to 1 to allow INSTALL_DIR under system paths (/usr, /bin, ...)

set -euo pipefail

# The whole script body lives inside main() so bash refuses to start executing
# anything until it has parsed the entire file. Defeats partial-download
# execution when `curl | bash` is interrupted mid-stream.
main() {
  local REPO="michal-franc/ai-workflow-engine"
  local INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
  local VERSION="${VERSION:-}"
  local FORCE="${FORCE:-0}"

  err()  { printf 'error: %s\n' "$*" >&2; exit 1; }
  info() { printf '==> %s\n' "$*"; }

  # `--proto =https` rejects any redirect that downgrades to http; `--tlsv1.2`
  # bans ancient negotiation. Defense-in-depth on top of GitHub's own TLS.
  curl_safe() { curl -fsSL --proto '=https' --tlsv1.2 "$@"; }

  detect_os() {
    case "$(uname -s)" in
      Linux*)  echo "linux" ;;
      Darwin*) echo "darwin" ;;
      *) err "unsupported OS: $(uname -s) (releases ship linux and darwin only)" ;;
    esac
  }

  detect_arch() {
    case "$(uname -m)" in
      x86_64|amd64)  echo "amd64" ;;
      arm64|aarch64) echo "arm64" ;;
      *) err "unsupported arch: $(uname -m)" ;;
    esac
  }

  validate_version() {
    case "$1" in
      v[0-9]*.[0-9]*.[0-9]*) ;;
      *) err "invalid version format: '$1' (expected vX.Y.Z)" ;;
    esac
    case "$1" in
      *..*|*/*|*\\*|*$' '*) err "invalid characters in version: '$1'" ;;
    esac
  }

  guard_install_dir() {
    local d="$1"
    case "$d" in
      /usr|/usr/*|/bin|/bin/*|/sbin|/sbin/*|/etc|/etc/*|/lib|/lib/*|/lib64|/lib64/*)
        if [ "$FORCE" != "1" ]; then
          err "refusing to install into system path '$d' (set FORCE=1 to override)"
        fi
        ;;
    esac
  }

  scan_tar_safe() {
    local archive="$1"
    if tar -tzf "$archive" | grep -E '^(/|.*/\.\./|\.\./)' >/dev/null; then
      err "archive '$archive' contains unsafe paths (zip-slip)"
    fi
  }

  local OS ARCH
  OS="$(detect_os)"
  ARCH="$(detect_arch)"

  guard_install_dir "$INSTALL_DIR"

  if [ -z "$VERSION" ]; then
    info "resolving latest release from github.com/$REPO"
    # Fetch into a variable first, then parse — piping curl into grep/head with
    # `set -o pipefail` would kill curl with SIGPIPE and abort the script.
    local JSON
    JSON="$(curl_safe "https://api.github.com/repos/$REPO/releases/latest")" \
      || err "could not fetch latest release metadata"
    VERSION="$(printf '%s\n' "$JSON" | sed -nE 's/.*"tag_name": *"([^"]+)".*/\1/p' | head -n1)"
    [ -n "$VERSION" ] || err "could not parse tag_name from release metadata"
  fi
  validate_version "$VERSION"

  local ASSET="issue-viewer_${VERSION}_${OS}_${ARCH}.tar.gz"
  local URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
  local SUMS_URL="${URL}.sha256"

  # TMP intentionally not `local`: the EXIT trap runs after main() returns,
  # at which point a function-scoped TMP would be unbound (failing under
  # `set -u`). A bare top-level assignment keeps the trap working.
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT

  info "downloading $ASSET"
  curl_safe -o "$TMP/$ASSET" "$URL" || err "download failed: $URL"

  info "verifying sha256 checksum"
  if ! curl_safe -o "$TMP/$ASSET.sha256" "$SUMS_URL"; then
    err "could not fetch checksum: $SUMS_URL"
  fi
  ( cd "$TMP" && sha256sum -c "$ASSET.sha256" >/dev/null ) \
    || err "checksum verification failed for $ASSET"

  info "extracting"
  cd "$TMP"
  scan_tar_safe "$ASSET"
  tar -xzf "$ASSET" --no-same-owner --no-same-permissions

  local STAGE="issue-viewer_${VERSION}_${OS}_${ARCH}"
  [ -d "$STAGE" ] || err "expected directory $STAGE not found in archive"

  mkdir -p "$INSTALL_DIR"
  install -m 0755 "$STAGE/issue-viewer" "$INSTALL_DIR/issue-viewer"
  install -m 0755 "$STAGE/issue-cli"    "$INSTALL_DIR/issue-cli"

  info "installed issue-viewer $VERSION → $INSTALL_DIR/issue-viewer"
  info "installed issue-cli    $VERSION → $INSTALL_DIR/issue-cli"

  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) printf '\nnote: %s is not on your PATH. Add this to your shell rc:\n  export PATH="%s:$PATH"\n' "$INSTALL_DIR" "$INSTALL_DIR" ;;
  esac
}

main "$@"
