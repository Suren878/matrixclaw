#!/usr/bin/env bash
set -euo pipefail

home_dir="${HOME:-}"
if [[ -z "$home_dir" ]]; then
  echo "uninstall.sh: HOME is not set" >&2
  exit 2
fi

install_dir="${MATRIXCLAW_INSTALL_DIR:-"$home_dir/.local/bin"}"
state_dir="${MATRIXCLAW_STATE_DIR:-"$home_dir/.local/state/matrixclaw"}"
# setup_config_dir is the directory matrixclaw setup keeps its files in, as
# the daemon resolves it: Go's os.UserConfigDir (macOS: ~/Library/Application
# Support; elsewhere an absolute $XDG_CONFIG_HOME, else ~/.config).
setup_config_dir() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    echo "$home_dir/Library/Application Support/matrixclaw"
  elif [[ "${XDG_CONFIG_HOME:-}" == /* ]]; then
    echo "$XDG_CONFIG_HOME/matrixclaw"
  else
    echo "$home_dir/.config/matrixclaw"
  fi
}

# has_tty succeeds when /dev/tty can really be opened; the node can exist
# without a controlling terminal.
has_tty() {
  (exec < /dev/tty) 2>/dev/null
}

config_dir="${MATRIXCLAW_CONFIG_DIR:-"$(setup_config_dir)"}"
purge=0
yes=0
daemon_running=0

usage() {
  cat <<'EOF'
Uninstall matrixclaw.

Usage:
  uninstall.sh [--purge] [--yes] [--install-dir DIR]

Options:
  --purge  Remove config and state in addition to binaries and service files.
  --yes    Do not prompt for purge confirmation.
EOF
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --purge)
      purge=1
      shift
      ;;
    --yes|-y)
      yes=1
      shift
      ;;
    --install-dir)
      if [[ "$#" -lt 2 ]]; then
        echo "uninstall.sh: --install-dir requires a value" >&2
        exit 2
      fi
      install_dir="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "uninstall.sh: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$install_dir" || "$install_dir" == "/" ]]; then
  echo "uninstall.sh: invalid install dir: ${install_dir:-<empty>}" >&2
  exit 2
fi
if [[ -z "$config_dir" || "$config_dir" == "/" || "$config_dir" == "$home_dir" ]]; then
  echo "uninstall.sh: invalid config dir: ${config_dir:-<empty>}" >&2
  exit 2
fi
if [[ -z "$state_dir" || "$state_dir" == "/" || "$state_dir" == "$home_dir" ]]; then
  echo "uninstall.sh: invalid state dir: ${state_dir:-<empty>}" >&2
  exit 2
fi

if [[ "$purge" == "1" && "$yes" != "1" ]]; then
  if ! has_tty; then
    echo "uninstall.sh: --purge requires an interactive terminal or --yes" >&2
    exit 2
  fi
  echo "This will remove matrixclaw config and state:"
  echo "  $config_dir"
  echo "  $state_dir"
  printf "Type 'purge matrixclaw' to continue: "
  read -r answer < /dev/tty
  if [[ "$answer" != "purge matrixclaw" ]]; then
    echo "uninstall.sh: purge cancelled"
    exit 1
  fi
fi

echo "[1/4] Stopping the daemon"
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user stop matrixclawd.service >/dev/null 2>&1 || true
  systemctl --user disable matrixclawd.service >/dev/null 2>&1 || true
fi
# A daemon that no service manager runs is stopped through its API.
if [[ -x "$install_dir/matrixclaw" ]]; then
  "$install_dir/matrixclaw" service stop >/dev/null 2>&1 || true
  if "$install_dir/matrixclaw" service status 2>/dev/null | grep -q 'service: Running'; then
    daemon_running=1
  fi
fi

echo "[2/4] Removing service files"
rm -f "$home_dir/.config/systemd/user/matrixclawd.service"
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user daemon-reload >/dev/null 2>&1 || true
fi

echo "[3/4] Removing binaries"
rm -f "$install_dir/matrixclaw" "$install_dir/matrixclawd" "$install_dir/matrixclaw-telephony-gateway"

echo "[4/4] Handling config and state"
if [[ "$purge" == "1" ]]; then
  rm -rf "$config_dir" "$state_dir"
  if [[ -n "${MATRIXCLAW_SETUP_PATH:-}" ]]; then
    rm -f "$MATRIXCLAW_SETUP_PATH" "$MATRIXCLAW_SETUP_PATH.pre-v4" "$(dirname "$MATRIXCLAW_SETUP_PATH")/daemon.env"
  fi
  # Older installs created this on macOS, where setup never lived in it.
  rmdir "$home_dir/.config/matrixclaw" 2>/dev/null || true
  echo "Removed config and state."
else
  echo "Kept config and state:"
  echo "  $config_dir"
  echo "  $state_dir"
  echo "Run uninstall.sh --purge to remove them."
fi

if [[ "$daemon_running" == "1" ]]; then
  echo "uninstall.sh: matrixclawd is still running; stop it (kill \$(pgrep -x matrixclawd)) before reinstalling" >&2
  echo "matrixclaw uninstalled, but the daemon is still running." >&2
  exit 1
fi
echo "matrixclaw uninstalled."
