#!/usr/bin/env bash
set -euo pipefail

SHA="${1:-${GITHUB_SHA:-}}"
REPO="${GITHUB_REPOSITORY:-}"

if [[ -z "$SHA" ]]; then
  echo "usage: require-green-checks.sh <commit-sha>" >&2
  exit 2
fi

if [[ -z "$REPO" ]]; then
  origin="$(git remote get-url origin 2>/dev/null || true)"
  if [[ "$origin" =~ github\.com[:/]([^/]+/[^/.]+) ]]; then
    REPO="${BASH_REMATCH[1]%.git}"
  else
    echo "GITHUB_REPOSITORY or git origin required" >&2
    exit 2
  fi
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI not found" >&2
  exit 2
fi

REQUIRED=(
  "test (ubuntu-latest)"
  "test (windows-latest)"
  "test (macos-latest)"
  "build binaries"
  "integration smoke"
  "validate scripts"
)

mapfile -t rows < <(
  gh api "/repos/${REPO}/commits/${SHA}/check-runs?per_page=100" \
    --jq '.check_runs[] | [.name, .status, .conclusion] | @tsv'
)

missing=0
failed=0

for want in "${REQUIRED[@]}"; do
  ok=0
  for row in "${rows[@]}"; do
    IFS=$'\t' read -r name status conclusion <<<"$row"
    [[ "$name" == "$want" ]] || continue
    ok=1
    if [[ "$status" != "completed" ]]; then
      echo "FAIL: $name still $status" >&2
      failed=1
    elif [[ "$conclusion" != "success" ]]; then
      echo "FAIL: $name conclusion=$conclusion" >&2
      failed=1
    else
      echo "OK: $name"
    fi
    break
  done
  if [[ "$ok" -eq 0 ]]; then
    echo "MISSING: $want" >&2
    missing=1
  fi
done

if [[ "$missing" -ne 0 || "$failed" -ne 0 ]]; then
  exit 1
fi

echo "All required checks green for ${SHA}"
