#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA="${RUNNER_TEMP:-/tmp}/ssh233-smoke-$$"
HTTP_PORT="${SSH233_SMOKE_HTTP_PORT:-6030}"
SSH_PORT="${SSH233_SMOKE_SSH_PORT:-6022}"
BASE="http://127.0.0.1:${HTTP_PORT}"

mkdir -p "$DATA"
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
YAML

cd "$ROOT"
go build -o "${DATA}/ssh233-server" ./cmd/server
"${DATA}/ssh233-server" -config "$CONFIG" &
PID=$!
trap 'kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 60); do
  if curl -fsS "${BASE}/health" 2>/dev/null | grep -q '"status":"ok"'; then
    break
  fi
  sleep 0.25
done
curl -fsS "${BASE}/health" | grep -q '"status":"ok"'
sleep 0.5

TOKEN=$(curl -fsS -X POST "${BASE}/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"root"}' | python3 -c "import json,sys; print(json.load(sys.stdin)['token'])")
[ -n "$TOKEN" ]

curl -fsS -H "Authorization: Bearer ${TOKEN}" "${BASE}/api/v1/tenants" | grep -q 'default'
curl -fsS -H "Authorization: Bearer ${TOKEN}" "${BASE}/api/v1/hosts" | grep -q '\['

curl -fsS -H "Authorization: Bearer ${TOKEN}" -X POST "${BASE}/api/v1/hosts" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-host","address":"127.0.0.1","port":22,"username":"root","enabled":true}' | grep -q 'smoke-host'

curl -fsS -o /dev/null -w '%{http_code}' "${BASE}/login.html" | grep -q '200'
curl -fsS -o /dev/null -w '%{http_code}' "${BASE}/manager.html" | grep -q '200'

curl -fsS -X POST "${BASE}/api/v1/agents/register" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-agent","register_token":"smoke-agent-token","tenant_slug":"default","hostname":"ci","ip":"127.0.0.1","version":"smoke"}' \
  | grep -q '"token"'

echo "ci-smoke: ok"
