#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${OUT_DIR:-"$repo_root/bin"}"
version="${VERSION:-$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)}"
commit="${COMMIT:-$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || echo "")}"
date="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
module_path="${MODULE_PATH:-$(go -C "$repo_root" list -m)}"

mkdir -p "$out_dir"

ldflags="-s -w"
ldflags="$ldflags -X ${module_path}/internal/version.Version=$version"
ldflags="$ldflags -X ${module_path}/internal/version.Commit=$commit"
ldflags="$ldflags -X ${module_path}/internal/version.Date=$date"

echo "building matrixclaw $version ($commit) -> $out_dir"
go build -trimpath -ldflags "$ldflags" -o "$out_dir/matrixclaw" "$repo_root/cmd/matrixclaw"
go build -trimpath -ldflags "$ldflags" -o "$out_dir/matrixclawd" "$repo_root/cmd/matrixclawd"
go build -trimpath -ldflags "$ldflags" -o "$out_dir/matrixclaw-telephony-gateway" "$repo_root/cmd/matrixclaw-telephony-gateway"

"$out_dir/matrixclaw" version || true
