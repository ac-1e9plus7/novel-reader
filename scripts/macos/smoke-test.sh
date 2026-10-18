#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  printf '%s\n' 'Execute this script; do not source it.' >&2
  return 1
fi
set -euo pipefail
if [[ "${1:-}" == '--help' ]]; then
  printf '%s\n' 'Usage: ./scripts/macos/smoke-test.sh' 'Use a free port and isolated data to verify start/status/stop, static frontend and SQLite.'
  exit 0
fi
[[ $# -eq 0 ]] || { printf '%s\n' 'Unexpected argument. Use --help.' >&2; exit 2; }
# shellcheck source=common.sh
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"
require_unix
require_command node
require_command curl
assert_stopped
mkdir -p "$reader_root/tmp"
test_data=$(mktemp -d "$reader_root/tmp/smoke.XXXXXX")
port=$(node -e 'const net=require("node:net");const server=net.createServer();server.listen(0,"127.0.0.1",()=>{console.log(server.address().port);server.close()});server.on("error",()=>process.exit(1));')
original_directory=$PWD
started=false
cleanup() {
  if [[ "$started" == true ]]; then bash "$reader_root/scripts/macos/start.sh" stop; fi
}
trap cleanup EXIT
LISTEN_ADDR="127.0.0.1:$port" DATA_DIR="$test_data" bash "$reader_root/scripts/macos/start.sh" start
started=true
[[ "$PWD" == "$original_directory" ]] || fail 'Start changed caller directory.'
healthy=false
for ((attempt=0; attempt<30; attempt++)); do
  if curl --fail --silent --max-time 2 "http://127.0.0.1:$port/api/health" > "$test_data/health.json"; then healthy=true; break; fi
  sleep 0.3
done
[[ "$healthy" == true ]] || fail 'Service health check failed.'
curl --fail --silent --max-time 5 "http://127.0.0.1:$port/" > "$test_data/index.html"
curl --fail --silent --max-time 5 "http://127.0.0.1:$port/api/books" > "$test_data/books.json"
node -e 'const fs=require("node:fs"),path=require("node:path");const root=process.argv[1];if(JSON.parse(fs.readFileSync(path.join(root,"health.json"))).status!=="ok"||JSON.parse(fs.readFileSync(path.join(root,"books.json"))).length!==0||!fs.readFileSync(path.join(root,"index.html"),"utf8").includes("id=\"root\""))process.exit(1);' "$test_data"
bash "$reader_root/scripts/macos/start.sh" status
bash "$reader_root/scripts/macos/start.sh" stop
started=false
[[ "$PWD" == "$original_directory" ]] || fail 'Stop changed caller directory.'
printf '%s\n' 'Startup, frontend, isolated SQLite, status, shutdown and directory checks passed.'
