#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="${ASTER_SETUP_JOURNEY_DIR:-${TMPDIR:-/tmp}/asterrouter-setup-journey-$$}"
PORT="${ASTER_SETUP_JOURNEY_PORT:-18088}"
BINARY="${ASTER_SETUP_JOURNEY_BINARY:-}"
DATABASE_URL="${DATABASE_URL:-}"
PID=""

if [ -d "${RUN_DIR}" ] && find "${RUN_DIR}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "Refusing to overwrite non-empty setup journey directory: ${RUN_DIR}" >&2
  exit 1
fi
if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "Setup journey port ${PORT} is already in use." >&2
  exit 1
fi

cleanup() {
  if [ -n "${PID}" ]; then
    kill -TERM "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "${RUN_DIR}"
if [ -z "${BINARY}" ]; then
  (
    cd "${ROOT_DIR}/frontend"
    npm run build
  )
else
  if [ ! -x "${BINARY}" ]; then
    echo "ASTER_SETUP_JOURNEY_BINARY must point to an executable: ${BINARY}" >&2
    exit 1
  fi
fi
(
  if [ -n "${BINARY}" ]; then
    cd "$(dirname "${BINARY}")"
    APP_COMMAND=("${BINARY}")
    FRONTEND_DIR="$(dirname "${BINARY}")/frontend/dist"
  else
    cd "${ROOT_DIR}/backend"
    APP_COMMAND=(go run ./cmd/asterrouter)
    FRONTEND_DIR="${ROOT_DIR}/frontend/dist"
  fi
  runtime_env=(
    "ASTER_ADDR=127.0.0.1:${PORT}"
    "ASTER_FRONTEND_DIR=${FRONTEND_DIR}"
    'ASTER_ADMIN_PASSWORD=setup-browser-test-password'
    'ASTER_SECRET_KEY=asterrouter-setup-browser-test-secret'
    "ASTER_PLUGIN_CACHE_DIR=${RUN_DIR}/data/plugin-cache"
    "ASTER_PLUGIN_ACTIVE_DIR=${RUN_DIR}/data/plugin-active"
    "ASTER_BACKUP_DIR=${RUN_DIR}/data/backups"
    "ASTER_DIAGNOSTIC_DIR=${RUN_DIR}/data/diagnostics"
  )
  if [ -n "${DATABASE_URL}" ]; then
    runtime_env+=("DATABASE_URL=${DATABASE_URL}")
  fi
  exec env "${runtime_env[@]}" "${APP_COMMAND[@]}"
) >"${RUN_DIR}/runtime.log" 2>&1 &
PID="$!"

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${PORT}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
    break
  fi
  if ! kill -0 "${PID}" >/dev/null 2>&1; then
    wait "${PID}" || true
    echo "Setup journey runtime exited before becoming ready." >&2
    exit 1
  fi
  sleep 0.25
done
curl -fsS "http://127.0.0.1:${PORT}/api/v1/setup/status" | grep -q '"setup_completed":false'

(
  cd "${ROOT_DIR}/frontend"
  CI=true \
    ASTER_E2E_INCLUDE_SETUP=1 \
    ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${PORT}" \
    ASTER_E2E_ARTIFACT_DIR="${RUN_DIR}/playwright" \
    npx playwright test --grep '@setup'
)

curl -fsS "http://127.0.0.1:${PORT}/api/v1/setup/status" | grep -q '"default_profile":"platform"'
curl -fsS "http://127.0.0.1:${PORT}/api/v1/setup/status" | grep -q '"setup_completed":true'

{
  echo 'setup_browser_journey=passed'
  echo "execution=$([ -n "${BINARY}" ] && echo candidate_binary || echo source_runtime)"
  echo 'profile=platform'
  echo 'browser=chromium'
} >"${RUN_DIR}/report.txt"

echo "Setup browser journey passed. Evidence: ${RUN_DIR}/report.txt"
