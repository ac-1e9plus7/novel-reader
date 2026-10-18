#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  printf '%s\n' 'Execute this script; do not source it.' >&2
  return 1
fi
set -euo pipefail
if [[ "${1:-}" == '--help' ]]; then
  printf '%s\n' 'Usage: ./scripts/macos/test.sh [--browser] [--race]' 'Run backend tests, vet and frontend build; optional Playwright and race detection.'
  exit 0
fi
browser=false
race=false
for arg in "$@"; do
  case "$arg" in --browser) browser=true ;; --race) race=true ;; *) printf 'Unknown argument: %s\n' "$arg" >&2; exit 2 ;; esac
done
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
require_unix
require_build_tools
(
  cd "$reader_root/backend"
  if [[ "$race" == true ]]; then go test -race ./...; else go test ./...; fi
  go vet ./...
  cd "$reader_root/frontend"
  npm run build
  if [[ "$browser" == true ]]; then npm run test:e2e; fi
)
