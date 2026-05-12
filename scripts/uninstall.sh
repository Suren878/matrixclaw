#!/usr/bin/env bash
set -euo pipefail

home_dir="${HOME:-}"
if [[ -z "$home_dir" ]]; then
  echo "uninstall.sh: HOME is not set" >&2
  exit 2
fi

install_dir="${MATRIXCLAW_INSTALL_DIR:-"$home_dir/.local/bin"}"
config_dir="${MATRIXCLAW_CONFIG_DIR:-"$home_dir/.config/matrixclaw"}"
state_dir="${MATRIXCLAW_STATE_DIR:-"$home_dir/.local/state/matrixclaw"}"
purge=0
yes=0

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
  if [[ ! -r /dev/tty ]]; then
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

echo "[1/4] Stopping user service if present"
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user stop matrixclawd.service >/dev/null 2>&1 || true
  systemctl --user disable matrixclawd.service >/dev/null 2>&1 || true
fi

echo "[2/4] Removing service files"
rm -f "$home_dir/.config/systemd/user/matrixclawd.service"
if command -v systemctl >/dev/null 2>&1; then
  systemctl --user daemon-reload >/dev/null 2>&1 || true
fi

echo "[3/4] Removing binaries"
rm -f "$install_dir/matrixclaw" "$install_dir/matrixclawd"

echo "[4/4] Handling config and state"
if [[ "$purge" == "1" ]]; then
  rm -rf "$config_dir" "$state_dir"
  echo "Removed config and state."
else
  echo "Kept config and state:"
  echo "  $config_dir"
  echo "  $state_dir"
  echo "Run uninstall.sh --purge to remove them."
fi

echo "matrixclaw uninstalled."
