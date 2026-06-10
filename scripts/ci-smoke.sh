#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA="${RUNNER_TEMP:-/tmp}/ssh233-smoke-$$"
HTTP_PORT="${SSH233_SMOKE_HTTP_PORT:-6030}"
SSH_PORT="${SSH233_SMOKE_SSH_PORT:-6022}"
BASE="http://127.0.0.1:${HTTP_PORT}"

mkdir -p "$DATA/logs"
CONFIG="${DATA}/config.yaml"
cat >"$CONFIG" <<YAML
server:
  http_addr: "127.0.0.1:${HTTP_PORT}"
  ssh_addr: "127.0.0.1:${SSH_PORT}"
database:
  driver: sqlite
  sqlite:
    path: ${DATA}/ssh233.db
auth:
  jwt_secret: smoke-test-secret
  token_ttl: 1h
  admin_user: root
  admin_password: root
ssh:
  host_key_path: ${DATA}/host_key
agent:
  register_token: smoke-agent-token
  heartbeat_ttl: 60s
logging:
  path: ${DATA}/logs/smoke.log
  max_size_mb: 1
  max_backups: 2
  level: info
YAML

cd "$ROOT"
go build -o "${DATA}/ssh233-server" ./cmd/server
BIN="${DATA}/ssh233-server"

echo "daemon start/stop..."
"$BIN" start -config "$CONFIG"
trap "$BIN stop -config $CONFIG 2>/dev/null || true" EXIT
"$BIN" status -config "$CONFIG" | grep -q 'status=running'
"$BIN" autostart-status -config "$CONFIG" | grep -q 'autostart_enabled=false'

echo "waiting for health..."
ready=0
for _ in $(seq 1 80); do
  if curl -fsS "${BASE}/health" 2>/dev/null | grep -q '"status":"ok"'; then
    ready=1
    break
  fi
  sleep 0.25
done
if [ "$ready" -ne 1 ]; then
  echo "server did not become healthy" >&2
  exit 1
fi

echo "login..."
LOGIN_JSON=$(curl -fsS -X POST "${BASE}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"root"}')
TOKEN=$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['token'])" "$LOGIN_JSON")
test -n "$TOKEN"

echo "list tenants..."
TENANTS=$(curl -fsS -H "Authorization: Bearer ${TOKEN}" "${BASE}/api/v1/tenants")
echo "$TENANTS" | grep -q 'default'
TENANT_ID=$(python3 -c "import json,sys; ts=json.loads(sys.argv[1]); print(next(t['id'] for t in ts if t.get('slug')=='default'))" "$TENANTS")

echo "create host..."
curl -fsS -H "Authorization: Bearer ${TOKEN}" -X POST "${BASE}/api/v1/hosts" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"smoke-host\",\"address\":\"127.0.0.1\",\"port\":22,\"username\":\"root\",\"tenant_id\":\"${TENANT_ID}\",\"enabled\":true}" \
  | grep -q 'smoke-host'

echo "register agent..."
curl -fsS -X POST "${BASE}/api/v1/agents/register" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-agent","register_token":"smoke-agent-token","tenant_slug":"default","hostname":"ci","ip":"127.0.0.1","version":"smoke"}' \
  | grep -q '"token"'

echo "audit stats..."
STATS=$(curl -fsS -H "Authorization: Bearer ${TOKEN}" "${BASE}/api/v1/audit/stats")
echo "$STATS" | grep -q '"total"'

echo "audit cleanup dry-run (older_than_days)..."
curl -fsS -H "Authorization: Bearer ${TOKEN}" -X DELETE "${BASE}/api/v1/audit?older_than_days=365" \
  | grep -q '"deleted"'

test -f "${DATA}/logs/smoke.log"

echo "cli version..."
"$BIN" version | grep -q 'ssh233-server'

echo "ci-smoke: ok"
