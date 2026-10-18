#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  printf '%s\n' 'Execute this script; do not source it.' >&2
  return 1
fi
set -euo pipefail
if [[ "${1:-}" == '--help' ]]; then
  printf '%s\n' 'Usage: ./scripts/macos/setup.sh' 'Install locked dependencies, build frontend/backend, create .env only if missing.'
  exit 0
fi
[[ $# -eq 0 ]] || { printf '%s\n' 'Unexpected argument. Use --help.' >&2; exit 2; }
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
require_unix
require_build_tools
assert_stopped
(
  cd "$reader_root"
  npm --prefix frontend ci
  npm --prefix frontend run build
  mkdir -p "$reader_root/bin"
  cd "$reader_root/backend"
  go mod download
  go build -trimpath -o "$reader_executable" ./cmd/server
)
if [[ ! -e "$reader_root/.env" ]]; then
  (umask 077; set -o noclobber; cat "$reader_root/.env.example" > "$reader_root/.env")
fi
printf '%s\n' 'Build complete. Configure .env, then run scripts/macos/start.sh start.'
