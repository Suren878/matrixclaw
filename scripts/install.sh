#!/usr/bin/env bash
set -euo pipefail

repo="${MATRIXCLAW_REPO:-Suren878/matrixclaw}"
version="${MATRIXCLAW_VERSION:-latest}"
install_dir="${MATRIXCLAW_INSTALL_DIR:-"$HOME/.local/bin"}"
run_setup="${MATRIXCLAW_RUN_SETUP:-1}"
from_source=0

usage() {
  cat <<'EOF'
Install matrixclaw.

Usage:
  install.sh [--version TAG] [--install-dir DIR] [--no-setup] [--from-source]

Environment:
  MATRIXCLAW_REPO         GitHub repo, default Suren878/matrixclaw
  MATRIXCLAW_VERSION      Release tag, default latest
  MATRIXCLAW_INSTALL_DIR  Binary directory, default ~/.local/bin
  MATRIXCLAW_RUN_SETUP    Set to 0 to skip matrixclaw setup
EOF
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --version)
      version="${2:-}"
      shift 2
      ;;
    --install-dir)
      install_dir="${2:-}"
      shift 2
      ;;
    --no-setup)
      run_setup=0
      shift
      ;;
    --from-source)
      from_source=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$install_dir" || "$install_dir" == "/" ]]; then
  echo "install.sh: invalid install dir: ${install_dir:-<empty>}" >&2
  exit 2
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "install.sh: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "install.sh: unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "install.sh: unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  local api="https://api.github.com/repos/${repo}/releases/latest"
  curl -fsSL "$api" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/; t print; b; :print; p; q'
}

install_from_source() {
  need_cmd go
  local root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  echo "[1/4] Building matrixclaw from source"
  mkdir -p "$install_dir"
  go -C "$root" build -o "$install_dir/matrixclaw" ./cmd/matrixclaw
  echo "[2/4] Building matrixclawd from source"
  go -C "$root" build -o "$install_dir/matrixclawd" ./cmd/matrixclawd
}

install_from_release() {
  need_cmd awk
  need_cmd curl
  need_cmd install
  need_cmd mktemp
  need_cmd tar
  need_cmd sed

  local os arch tag archive url checksum_url tmp
  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$version"
  if [[ "$tag" == "latest" ]]; then
    tag="$(latest_tag)"
  fi
  if [[ -z "$tag" ]]; then
    echo "install.sh: could not resolve release tag for ${repo}" >&2
    exit 1
  fi

  archive="matrixclaw-${tag}-${os}-${arch}.tar.gz"
  url="https://github.com/${repo}/releases/download/${tag}/${archive}"
  checksum_url="https://github.com/${repo}/releases/download/${tag}/checksums.txt"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  echo "[1/4] Downloading ${archive}"
  curl -fL "$url" -o "$tmp/$archive"
  if curl -fsSL "$checksum_url" -o "$tmp/checksums.txt"; then
    local expected actual
    expected="$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/checksums.txt")"
    if [[ -z "$expected" ]]; then
      echo "install.sh: checksum not found for ${archive}" >&2
      exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      (cd "$tmp" && awk -v f="$archive" '$2 == f {print $0}' checksums.txt | sha256sum -c - >/dev/null)
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
      if [[ "$actual" != "$expected" ]]; then
        echo "install.sh: checksum mismatch for ${archive}" >&2
        exit 1
      fi
    else
      echo "install.sh: warning: sha256sum/shasum not found; checksum not verified" >&2
    fi
  else
    echo "install.sh: warning: checksums.txt not found for ${tag}; checksum not verified" >&2
  fi

  echo "[2/4] Installing binaries to ${install_dir}"
  mkdir -p "$install_dir"
  tar -xzf "$tmp/$archive" -C "$tmp"
  install -m 0755 "$tmp/matrixclaw" "$install_dir/matrixclaw"
  install -m 0755 "$tmp/matrixclawd" "$install_dir/matrixclawd"
}

if [[ "$from_source" == "1" ]]; then
  install_from_source
else
  install_from_release
fi

echo "[3/4] Preparing local directories"
mkdir -p "$HOME/.config/matrixclaw" "$HOME/.local/state/matrixclaw"

echo "[4/4] Installed matrixclaw"
echo "  $install_dir/matrixclaw"
echo "  $install_dir/matrixclawd"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *)
    echo
    echo "Add this directory to PATH if needed:"
    echo "  export PATH=\"$install_dir:\$PATH\""
    ;;
esac

if [[ "$run_setup" == "1" ]]; then
  echo
  echo "Starting setup..."
  if [[ -r /dev/tty ]]; then
    exec "$install_dir/matrixclaw" setup < /dev/tty
  fi
  echo "install.sh: no interactive terminal available; run setup manually:"
  echo "  $install_dir/matrixclaw setup"
  exit 0
fi

echo
echo "Next:"
echo "  $install_dir/matrixclaw setup"
