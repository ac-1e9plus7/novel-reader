#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  printf '%s\n' 'Execute this script; do not source it.' >&2
  return 1
fi
set -euo pipefail
if [[ "${1:-}" == '--help' ]]; then
  printf '%s\n' 'Usage: ./scripts/macos/start.sh start|stop|status' 'Reads .env in the project root; environment variables override it.' 'Example: LISTEN_ADDR=127.0.0.1:9000 ./scripts/macos/start.sh start'
  exit 0
fi
[[ $# -eq 1 ]] || { printf '%s\n' 'Expected start, stop or status. Use --help.' >&2; exit 2; }
action=$1
case "$action" in start|stop|status) ;; *) printf '%s\n' 'Unknown action. Use --help.' >&2; exit 2 ;; esac
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
require_unix
require_command lsof
require_command ps
mkdir -p "$reader_root/logs"
control_lock="$reader_root/logs/.macos-control.lock"
mkdir "$control_lock" 2>/dev/null || fail "Another service command is active. If a command crashed, verify it has exited before removing $control_lock."
release_lock() { rmdir "$control_lock" 2>/dev/null || true; }
trap release_lock EXIT

if load_state; then
  if process_alive "$reader_pid"; then
    owns_process || fail 'Process identity changed; refusing to operate on an unrelated process.'
    if [[ "$action" == 'stop' ]]; then
      kill -TERM "$reader_pid"
      for ((attempt=0; attempt<100; attempt++)); do
        if ! process_alive "$reader_pid"; then
          rm -f "$reader_state"
          printf '%s\n' 'Server stopped.'
          exit 0
        fi
        # PID reuse must never lead to another signal being sent.
        [[ "$(process_started "$reader_pid")" == "$reader_started" ]] || fail 'Process identity changed while stopping; inspect the state file.'
        sleep 0.1
      done
      fail 'Server did not exit within 10 seconds. State retained; inspect logs before retrying.'
    fi
    printf 'Server is running (PID %s).\n' "$reader_pid"
    exit 0
  fi
  rm -f "$reader_state"
fi
if [[ "$action" != 'start' ]]; then
  printf '%s\n' 'No managed server is running.'
  exit 0
fi
[[ -x "$reader_executable" ]] || fail 'Run scripts/macos/setup.sh first.'
[[ -f "$reader_root/frontend/dist/index.html" ]] || fail 'Frontend build is missing; run scripts/macos/setup.sh.'
stamp=$(date '+%Y%m%d-%H%M%S')
stdout="$reader_root/logs/$stamp-$$.stdout.log"
stderr="$reader_root/logs/$stamp-$$.stderr.log"
(
  cd "$reader_root"
  exec nohup "$reader_executable"
) </dev/null >"$stdout" 2>"$stderr" &
reader_pid=$!
reader_started=$(process_started "$reader_pid")
sleep 1
process_alive "$reader_pid" || fail "Server did not start. See $stderr"
owns_process || fail "Could not verify the new process. Inspect PID $reader_pid and $stderr."
state_temp="$reader_state.$$.tmp"
(umask 077; printf '%s\n%s\n' "$reader_pid" "$reader_started" > "$state_temp")
mv -f "$state_temp" "$reader_state"
printf 'Server started (PID %s). Listener address and errors: %s\n' "$reader_pid" "$stderr"
