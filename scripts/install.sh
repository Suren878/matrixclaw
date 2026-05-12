#!/usr/bin/env bash
set -euo pipefail

repo="${MATRIXCLAW_REPO:-Suren878/matrixclaw}"
home_dir="${HOME:-}"
if [[ -z "$home_dir" ]]; then
  echo "install.sh: HOME is not set" >&2
  exit 2
fi

version="${MATRIXCLAW_VERSION:-latest}"
install_dir="${MATRIXCLAW_INSTALL_DIR:-"$home_dir/.local/bin"}"
run_setup="${MATRIXCLAW_RUN_SETUP:-1}"
from_source=0
self_test=0
release_tmp=""

cleanup_release_tmp() {
  if [[ -n "${release_tmp:-}" ]]; then
    rm -rf "$release_tmp"
  fi
}

usage() {
  cat <<'EOF'
Install matrixclaw.

Usage:
  install.sh [--version TAG] [--install-dir DIR] [--no-setup] [--from-source] [--self-test]

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
      if [[ "$#" -lt 2 ]]; then
        echo "install.sh: --version requires a value" >&2
        exit 2
      fi
      version="${2:-}"
      shift 2
      ;;
    --install-dir)
      if [[ "$#" -lt 2 ]]; then
        echo "install.sh: --install-dir requires a value" >&2
        exit 2
      fi
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
    --self-test)
      self_test=1
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

detect_os_name() {
  case "$1" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "install.sh: unsupported OS: $1" >&2
      exit 1
      ;;
  esac
}

detect_os() {
  detect_os_name "$(uname -s)"
}

detect_arch_name() {
  case "$1" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "install.sh: unsupported architecture: $1" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  detect_arch_name "$(uname -m)"
}

latest_tag() {
  local api="https://api.github.com/repos/${repo}/releases/latest"
  local response
  response="$(curl -fsSL "$api")"
  printf '%s\n' "$response" | parse_latest_tag
}

parse_latest_tag_awk() {
  awk -F'"' '/"tag_name"[[:space:]]*:/ && tag == "" {tag = $4} END {print tag}'
}

parse_latest_tag() {
  if command -v jq >/dev/null 2>&1; then
    jq -r '.tag_name // empty'
  else
    parse_latest_tag_awk
  fi
}

archive_name() {
  printf 'matrixclaw-%s-%s-%s.tar.gz\n' "$1" "$2" "$3"
}

make_tmp_dir() {
  local base="${TMPDIR:-/tmp}"
  mktemp -d 2>/dev/null || mktemp -d "${base%/}/matrixclaw.XXXXXX"
}

self_test_equal() {
  local name="$1"
  local got="$2"
  local want="$3"
  if [[ "$got" != "$want" ]]; then
    echo "install.sh: self-test failed: ${name}: got ${got:-<empty>}, want ${want}" >&2
    exit 1
  fi
}

run_self_test() {
  local got tmp

  got="$(printf '%s\n' '{"tag_name":"v1.2.3"}' | parse_latest_tag_awk)"
  self_test_equal "awk compact tag parser" "$got" "v1.2.3"

  got="$(printf '%s\n' '{' '  "name": "release",' '  "tag_name" : "v0.1.2",' '  "body": "ok"' '}' | parse_latest_tag_awk)"
  self_test_equal "awk spaced tag parser" "$got" "v0.1.2"

  got="$({ printf '%s\n' '{"tag_name":"v9.8.7"}'; awk 'BEGIN {for (i = 0; i < 20000; i++) print "padding"}'; } | parse_latest_tag_awk)"
  self_test_equal "awk long tag parser" "$got" "v9.8.7"

  got="$(printf '%s\n' '{"tag_name":"v2.0.0"}' | parse_latest_tag)"
  self_test_equal "latest tag parser" "$got" "v2.0.0"

  got="$(archive_name "v1.2.3" "darwin" "arm64")"
  self_test_equal "archive name" "$got" "matrixclaw-v1.2.3-darwin-arm64.tar.gz"

  got="$(detect_os_name "Darwin")"
  self_test_equal "darwin OS mapping" "$got" "darwin"

  got="$(detect_os_name "Linux")"
  self_test_equal "linux OS mapping" "$got" "linux"

  got="$(detect_arch_name "x86_64")"
  self_test_equal "x86_64 arch mapping" "$got" "amd64"

  got="$(detect_arch_name "aarch64")"
  self_test_equal "aarch64 arch mapping" "$got" "arm64"

  got="$(detect_arch_name "arm64")"
  self_test_equal "arm64 arch mapping" "$got" "arm64"

  tmp="$(make_tmp_dir)"
  if [[ ! -d "$tmp" ]]; then
    echo "install.sh: self-test failed: make_tmp_dir did not create a directory" >&2
    exit 1
  fi
  rm -rf "$tmp"
  echo "install.sh: self-test passed"
}

install_from_source() {
  need_cmd go
  local root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
  if [[ ! -d "$root/cmd/matrixclaw" || ! -d "$root/cmd/matrixclawd" ]]; then
    echo "install.sh: --from-source requires a matrixclaw source checkout" >&2
    exit 1
  fi
  echo "[1/4] Building matrixclaw from source"
  mkdir -p "$install_dir"
  (cd "$root" && go build -o "$install_dir/matrixclaw" ./cmd/matrixclaw)
  echo "[2/4] Building matrixclawd from source"
  (cd "$root" && go build -o "$install_dir/matrixclawd" ./cmd/matrixclawd)
}

install_from_release() {
  need_cmd awk
  need_cmd curl
  need_cmd install
  need_cmd mktemp
  need_cmd tar

  local os arch tag archive url checksum_url
  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$version"
  if [[ "$tag" == "latest" ]]; then
    if ! tag="$(latest_tag)"; then
      echo "install.sh: could not resolve release tag for ${repo}" >&2
      exit 1
    fi
  fi
  if [[ -z "$tag" ]]; then
    echo "install.sh: could not resolve release tag for ${repo}" >&2
    exit 1
  fi

  archive="$(archive_name "$tag" "$os" "$arch")"
  url="https://github.com/${repo}/releases/download/${tag}/${archive}"
  checksum_url="https://github.com/${repo}/releases/download/${tag}/checksums.txt"
  release_tmp="$(make_tmp_dir)"
  trap cleanup_release_tmp EXIT

  echo "[1/4] Downloading ${archive}"
  curl -fL "$url" -o "$release_tmp/$archive"
  if curl -fsSL "$checksum_url" -o "$release_tmp/checksums.txt"; then
    local expected actual
    expected="$(awk -v f="$archive" '$2 == f {print $1}' "$release_tmp/checksums.txt")"
    if [[ -z "$expected" ]]; then
      echo "install.sh: checksum not found for ${archive}" >&2
      exit 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      (cd "$release_tmp" && awk -v f="$archive" '$2 == f {print $0}' checksums.txt | sha256sum -c - >/dev/null)
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$release_tmp/$archive" | awk '{print $1}')"
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
  tar -xzf "$release_tmp/$archive" -C "$release_tmp"
  install -m 0755 "$release_tmp/matrixclaw" "$install_dir/matrixclaw"
  install -m 0755 "$release_tmp/matrixclawd" "$install_dir/matrixclawd"
}

if [[ "$self_test" == "1" ]]; then
  run_self_test
  exit 0
fi

if [[ "$from_source" == "1" ]]; then
  install_from_source
else
  install_from_release
fi

echo "[3/4] Preparing local directories"
mkdir -p "$home_dir/.config/matrixclaw" "$home_dir/.local/state/matrixclaw"

echo "[4/4] Installed matrixclaw"
echo "  $install_dir/matrixclaw"
echo "  $install_dir/matrixclawd"

case ":${PATH:-}:" in
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
