#!/usr/bin/env bash
# SSH233 Agent Server — cross-platform installer (Linux / macOS / Windows via Git Bash)
# Usage:
#   curl -fsSL .../install.sh | bash
#   curl -fsSL .../install.sh | bash -s -- --from-source
#   bash scripts/install.sh --version v1.0.0

set -euo pipefail

REPO="${SSH233_REPO:-neko233-com/ssh233-agent-server}"
BINARY="ssh233-server"
VERSION="${SSH233_VERSION:-latest}"
FROM_SOURCE=0
INSTALL_DIR="${SSH233_INSTALL:-${HOME}/.local/share/ssh233}"
CONFIG_DIR="${SSH233_CONFIG:-${HOME}/.config/ssh233}"

usage() {
  cat <<EOF
SSH233 Agent Server installer

Options:
  --from-source     Build from local git checkout (must run inside repo)
  --version VER     Release version (default: latest)
  --install-dir DIR Install directory (default: ~/.local/share/ssh233)
  --help            Show help

Examples:
  bash scripts/install.sh
  bash scripts/install.sh --from-source
  SSH233_INSTALL=/opt/ssh233 bash scripts/install.sh --version v0.1.0
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --from-source) FROM_SOURCE=1; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown arg: $1"; usage; exit 1 ;;
  esac
done

detect_os() {
  case "$(uname -s)" in
    Linux*) echo linux ;;
    Darwin*) echo darwin ;;
    MINGW*|MSYS*|CYGWIN*) echo windows ;;
    *) echo unsupported ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo amd64 ;;
  esac
}

OS=$(detect_os)
ARCH=$(detect_arch)
[[ "$OS" == unsupported ]] && { echo "Unsupported OS"; exit 1; }

EXT=""
[[ "$OS" == windows ]] && EXT=".exe"
TARGET="${INSTALL_DIR}/${BINARY}${EXT}"

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR/data"

if [[ "$FROM_SOURCE" -eq 1 ]]; then
  ROOT="$(cd "$(dirname "$0")/.." && pwd)"
  command -v go >/dev/null || { echo "Go is required for --from-source"; exit 1; }
  echo "Building from source: $ROOT"
  (cd "$ROOT" && go build -o "$TARGET" ./cmd/server)
else
  if [[ "$VERSION" == latest ]]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"v([^"]+)".*/\1/' || echo "0.1.0")
  fi
  VERSION="${VERSION#v}"
  ASSET="${BINARY}-${OS}-${ARCH}${EXT}"
  URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"
  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  echo "Downloading ${URL}"
  if ! curl -fsSL "$URL" -o "${TMP}/${BINARY}${EXT}"; then
    echo "Release download failed — try: bash scripts/install.sh --from-source"
    exit 1
  fi
  install -m 0755 "${TMP}/${BINARY}${EXT}" "$TARGET"
fi

if [[ ! -f "${CONFIG_DIR}/config.yaml" ]]; then
  if [[ -f "$(dirname "$0")/../config.example.yaml" ]]; then
    cp "$(dirname "$0")/../config.example.yaml" "${CONFIG_DIR}/config.yaml"
  else
    cat >"${CONFIG_DIR}/config.yaml" <<YAML
server:
  http_addr: ":6030"
  ssh_addr: ":2222"
database:
  driver: sqlite
  sqlite:
    path: ${CONFIG_DIR}/data/ssh233.db
auth:
  jwt_secret: change-me
  token_ttl: 24h
  admin_user: root
  admin_password: root
ssh:
  host_key_path: ${CONFIG_DIR}/data/host_key
agent:
  register_token: change-me
  heartbeat_ttl: 60s
YAML
  fi
fi

cat <<EOF

Installed: ${TARGET}
Config:    ${CONFIG_DIR}/config.yaml

Start:
  ${TARGET} -config ${CONFIG_DIR}/config.yaml

Web UI:  http://127.0.0.1:6030/login.html
Admin:   root / root

Add to PATH (optional):
  export PATH="${INSTALL_DIR}:\$PATH"
EOF
