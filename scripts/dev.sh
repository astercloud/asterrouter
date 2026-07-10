#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PIDS=()

cleanup() {
  if [ "${#PIDS[@]}" -gt 0 ]; then
    kill "${PIDS[@]}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

(
  cd "${ROOT_DIR}/backend"
  ASTER_FRONTEND_DIR="${ASTER_FRONTEND_DIR:-../frontend/dist}" go run ./cmd/asterrouter
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}/frontend"
  npm run dev -- --host 0.0.0.0
) &
PIDS+=("$!")

while true; do
  for pid in "${PIDS[@]}"; do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      wait "${pid}"
      exit "$?"
    fi
  done
  sleep 1
done
