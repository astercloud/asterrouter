#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -f "${ROOT_DIR}/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "${ROOT_DIR}/.env"
  set +a
fi

BACKEND_HOST="${ASTER_DEV_BACKEND_HOST:-127.0.0.1}"
BACKEND_PORT="${ASTER_DEV_BACKEND_PORT:-8080}"
FRONTEND_HOST="${ASTER_DEV_FRONTEND_HOST:-0.0.0.0}"
FRONTEND_PORT="${ASTER_DEV_FRONTEND_PORT:-5173}"
BACKEND_URL="http://${BACKEND_HOST}:${BACKEND_PORT}"
PIDS=()

cleanup() {
  if [ "${#PIDS[@]}" -gt 0 ]; then
    local pid
    for pid in "${PIDS[@]}"; do
      kill "${pid}" >/dev/null 2>&1 || true
    done
    for pid in "${PIDS[@]}"; do
      wait "${pid}" >/dev/null 2>&1 || true
    done
  fi
}

trap cleanup EXIT INT TERM

port_in_use() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "${port}" >/dev/null 2>&1
    return $?
  fi
  return 1
}

require_free_port() {
  local name="$1"
  local port="$2"
  if port_in_use "${port}"; then
    echo "${name} port ${port} is already in use. Override it with ASTER_DEV_${name}_PORT." >&2
    exit 1
  fi
}

require_free_port "BACKEND" "${BACKEND_PORT}"
require_free_port "FRONTEND" "${FRONTEND_PORT}"

echo "AsterRouter API: ${BACKEND_URL}"
echo "AsterRouter UI:  http://${FRONTEND_HOST}:${FRONTEND_PORT}"

(
  cd "${ROOT_DIR}/backend"
  ASTER_ADDR="${ASTER_ADDR:-${BACKEND_HOST}:${BACKEND_PORT}}" \
    ASTER_FRONTEND_DIR="${ASTER_FRONTEND_DIR:-../frontend/dist}" \
    go run ./cmd/asterrouter
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}/frontend"
  VITE_DEV_PROXY_TARGET="${VITE_DEV_PROXY_TARGET:-${BACKEND_URL}}" \
    VITE_DEV_PORT="${VITE_DEV_PORT:-${FRONTEND_PORT}}" \
    npm run dev -- --host "${FRONTEND_HOST}" --port "${FRONTEND_PORT}"
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
