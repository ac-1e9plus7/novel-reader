#!/usr/bin/env bash
# Shared helpers use only Bash 3.2 features (the macOS system Bash).
reader_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
reader_executable="$reader_root/bin/novel-reader"
reader_state="$reader_root/logs/server-macos.pid"

fail() { printf '%s\n' "$*" >&2; exit 1; }
require_command() { command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"; }
require_unix() {
  case "$(uname -s)" in
    Darwin|Linux) ;;
    *) fail 'Run these scripts on macOS/Linux. On Windows use scripts/windows; Git Bash supports syntax checks only.' ;;
  esac
}
require_build_tools() {
  require_command go
  require_command node
  require_command npm
  local version major minor
  version=$(go env GOVERSION)
  if [[ "$version" =~ ^go([0-9]+)\.([0-9]+) ]]; then
    major=${BASH_REMATCH[1]}; minor=${BASH_REMATCH[2]}
    (( major > 1 || (major == 1 && minor >= 24) )) || fail 'Go 1.24+ is required.'
  else
    fail 'Cannot verify Go version; install Go 1.24+.'
  fi
  node -e 'if (Number(process.versions.node.split(".")[0]) < 24) process.exit(1)' || fail 'Node.js 24+ is required.'
}
process_started() { LC_ALL=C ps -p "$1" -o lstart= 2>/dev/null; }
process_alive() {
  local state
  state=$(ps -p "$1" -o stat= 2>/dev/null) || return 1
  [[ -n "$state" && "$state" != *Z* ]] && kill -0 "$1" 2>/dev/null
}
load_state() {
  [[ -f "$reader_state" ]] || return 1
  IFS= read -r reader_pid < "$reader_state" || return 1
  reader_started=$(sed -n '2p' "$reader_state")
  [[ "$reader_pid" =~ ^[1-9][0-9]*$ && "$reader_pid" -gt 1 && -n "$reader_started" ]] || fail "Invalid service state: $reader_state"
}
owns_process() {
  process_alive "$reader_pid" || return 1
  [[ "$(process_started "$reader_pid")" == "$reader_started" ]] || return 1
  # Verify executable identity as well as PID/start time before sending a signal.
  lsof -a -p "$reader_pid" -d txt -Fn 2>/dev/null | grep -Fx "n$reader_executable" >/dev/null
}
assert_stopped() {
  if load_state && process_alive "$reader_pid"; then
    fail 'A recorded server process is running. Stop it before building or testing the service lifecycle.'
  fi
}
