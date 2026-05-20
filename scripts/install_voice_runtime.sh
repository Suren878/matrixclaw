#!/usr/bin/env bash
set -euo pipefail

home_dir="${HOME:-}"
if [[ -z "$home_dir" ]]; then
  echo "install_voice_runtime.sh: HOME is not set" >&2
  exit 2
fi

state_home="${XDG_STATE_HOME:-"$home_dir/.local/state"}"
state_dir="${MATRIXCLAW_STATE_DIR:-"$state_home/matrixclaw"}"
runtime_dir="${MATRIXCLAW_RUNTIME_DIR:-"$state_dir/runtime"}"
whisper_repo="${MATRIXCLAW_WHISPER_CPP_REPO:-https://github.com/ggml-org/whisper.cpp.git}"
install_piper=1
install_whisper=1
install_supertonic=1
install_system_deps=1
self_test=0
target_set=0

usage() {
  cat <<'EOF'
Install local matrixclaw voice runtimes.

Usage:
  install_voice_runtime.sh [--piper] [--whisper] [--supertonic] [--all] [--no-system-deps] [--self-test]

Environment:
  MATRIXCLAW_STATE_DIR          State directory, default ~/.local/state/matrixclaw
  MATRIXCLAW_RUNTIME_DIR        Runtime directory, default $MATRIXCLAW_STATE_DIR/runtime
  MATRIXCLAW_WHISPER_CPP_REPO   whisper.cpp git URL, default https://github.com/ggml-org/whisper.cpp.git
EOF
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --piper)
      if [[ "$target_set" == "0" ]]; then
        install_piper=0
        install_whisper=0
        install_supertonic=0
        target_set=1
      fi
      install_piper=1
      shift
      ;;
    --whisper)
      if [[ "$target_set" == "0" ]]; then
        install_piper=0
        install_whisper=0
        install_supertonic=0
        target_set=1
      fi
      install_whisper=1
      shift
      ;;
    --supertonic)
      if [[ "$target_set" == "0" ]]; then
        install_piper=0
        install_whisper=0
        install_supertonic=0
        target_set=1
      fi
      install_supertonic=1
      shift
      ;;
    --all)
      install_piper=1
      install_whisper=1
      install_supertonic=1
      target_set=1
      shift
      ;;
    --no-system-deps)
      install_system_deps=0
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
      echo "install_voice_runtime.sh: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "install_voice_runtime.sh: required command not found: $1" >&2
    exit 1
  fi
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

brew_cmd() {
  if have_cmd brew; then
    command -v brew
    return 0
  fi
  for candidate in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    if [[ -x "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

need_cxx_compiler() {
  if have_cmd c++ || have_cmd clang++ || have_cmd g++; then
    return
  fi
  if [[ "$(detect_os)" == "darwin" ]]; then
    echo "install_voice_runtime.sh: C++ compiler not found; install Xcode Command Line Tools with: xcode-select --install" >&2
  else
    echo "install_voice_runtime.sh: C++ compiler not found; install g++ or clang++ and rerun" >&2
  fi
  exit 1
}

sudo_cmd() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  elif have_cmd sudo; then
    sudo "$@"
  else
    echo "install_voice_runtime.sh: $1 requires root; install sudo or run as root" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *)
      echo "install_voice_runtime.sh: unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

install_system_packages() {
  if [[ "$install_system_deps" != "1" ]]; then
    return
  fi
  local os brew
  os="$(detect_os)"
  case "$os" in
    linux)
      if have_cmd apt-get; then
        sudo_cmd apt-get update
        sudo_cmd apt-get install -y git cmake curl g++ make python3 python3-venv ffmpeg
      elif have_cmd dnf; then
        sudo_cmd dnf install -y git cmake curl gcc-c++ make python3 ffmpeg
      elif have_cmd pacman; then
        sudo_cmd pacman -Sy --needed git cmake curl gcc make python python-pip ffmpeg
      else
        echo "install_voice_runtime.sh: install git, cmake, a C++ compiler, python3, python3-venv, and ffmpeg, then rerun with --no-system-deps" >&2
        exit 1
      fi
      ;;
    darwin)
      if have_cmd xcode-select && ! xcode-select -p >/dev/null 2>&1; then
        echo "install_voice_runtime.sh: Xcode Command Line Tools are required on macOS; run: xcode-select --install" >&2
        exit 1
      fi
      if ! brew="$(brew_cmd)"; then
        echo "install_voice_runtime.sh: Homebrew is required on macOS for voice runtime dependencies" >&2
        exit 1
      fi
      "$brew" install git cmake curl python ffmpeg
      ;;
  esac
}

piper_binary() {
  printf '%s/piper-venv/bin/piper\n' "$runtime_dir"
}

whisper_binary() {
  printf '%s/whisper.cpp/build/bin/whisper-cli\n' "$runtime_dir"
}

whisper_server_binary() {
  printf '%s/whisper.cpp/build/bin/whisper-server\n' "$runtime_dir"
}

supertonic_binary() {
  printf '%s/supertonic-venv/bin/supertonic\n' "$runtime_dir"
}

install_piper_runtime() {
  local binary
  binary="$(piper_binary)"
  if [[ -x "$binary" ]]; then
    echo "Piper runtime already installed: $binary"
    return
  fi
  need_cmd python3
  mkdir -p "$runtime_dir"
  python3 -m venv "$runtime_dir/piper-venv"
  "$runtime_dir/piper-venv/bin/python" -m pip install --upgrade pip
  "$runtime_dir/piper-venv/bin/pip" install piper-tts
  if [[ ! -x "$binary" ]]; then
    echo "install_voice_runtime.sh: Piper install finished but binary was not found: $binary" >&2
    exit 1
  fi
  echo "Piper runtime installed: $binary"
}

install_whisper_runtime() {
  local binary server_binary source_dir
  binary="$(whisper_binary)"
  server_binary="$(whisper_server_binary)"
  source_dir="$runtime_dir/whisper.cpp"
  if [[ -x "$binary" && -x "$server_binary" ]]; then
    echo "Whisper.cpp runtime already installed: $binary"
    echo "Whisper.cpp server already installed: $server_binary"
    return
  fi
  need_cmd git
  need_cmd cmake
  need_cxx_compiler
  mkdir -p "$runtime_dir"
  if [[ -d "$source_dir/.git" ]]; then
    git -C "$source_dir" fetch --depth 1 origin
    git -C "$source_dir" reset --hard FETCH_HEAD
  elif [[ -e "$source_dir" ]]; then
    echo "install_voice_runtime.sh: $source_dir exists but is not a git checkout" >&2
    exit 1
  else
    git clone --depth 1 "$whisper_repo" "$source_dir"
  fi
  cmake -S "$source_dir" -B "$source_dir/build" -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=ON -DCMAKE_BUILD_TYPE=Release
  cmake --build "$source_dir/build" -j 4 --config Release --target whisper-cli whisper-server
  if [[ ! -x "$binary" ]]; then
    echo "install_voice_runtime.sh: Whisper.cpp build finished but binary was not found: $binary" >&2
    exit 1
  fi
  if [[ ! -x "$server_binary" ]]; then
    echo "install_voice_runtime.sh: Whisper.cpp build finished but server binary was not found: $server_binary" >&2
    exit 1
  fi
  echo "Whisper.cpp runtime installed: $binary"
  echo "Whisper.cpp server installed: $server_binary"
}

install_supertonic_runtime() {
  local binary
  binary="$(supertonic_binary)"
  need_cmd python3
  mkdir -p "$runtime_dir"
  python3 -m venv "$runtime_dir/supertonic-venv"
  "$runtime_dir/supertonic-venv/bin/python" -m pip install --upgrade pip
  "$runtime_dir/supertonic-venv/bin/pip" install 'supertonic[serve]'
  if [[ ! -x "$binary" ]]; then
    echo "install_voice_runtime.sh: Supertonic install finished but binary was not found: $binary" >&2
    exit 1
  fi
  "$binary" download
  echo "Supertonic runtime installed: $binary"
}

run_self_test() {
  local tmp
  tmp="$(mktemp -d)"
  MATRIXCLAW_RUNTIME_DIR="$tmp/runtime" bash "$0" --no-system-deps --help >/dev/null
  rm -rf "$tmp"
  echo "install_voice_runtime.sh: self-test passed"
}

if [[ "$self_test" == "1" ]]; then
  run_self_test
  exit 0
fi

install_system_packages
mkdir -p "$runtime_dir"

if [[ "$install_piper" == "1" ]]; then
  install_piper_runtime
fi
if [[ "$install_whisper" == "1" ]]; then
  install_whisper_runtime
fi
if [[ "$install_supertonic" == "1" ]]; then
  install_supertonic_runtime
fi

echo "Local voice runtime is ready."
