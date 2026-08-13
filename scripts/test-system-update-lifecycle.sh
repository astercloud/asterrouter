#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_PORT="${ASTER_SYSTEM_UPDATE_E2E_PORT:-48180}"
OFFICIAL_PORT="${ASTER_SYSTEM_UPDATE_E2E_OFFICIAL_PORT:-49180}"
POSTGRES_PORT="${ASTER_SYSTEM_UPDATE_E2E_POSTGRES_PORT:-55445}"
POSTGRES_SOCKET_DIR="${ASTER_SYSTEM_UPDATE_E2E_POSTGRES_SOCKET_DIR:-/tmp}"
DATABASE_NAME="asterrouter_e2e_system_update"
OLD_VERSION="${ASTER_SYSTEM_UPDATE_E2E_OLD_VERSION:-$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")}"
DEFAULT_NEW_VERSION="$(python3 - "${OLD_VERSION}" <<'PY'
import re
import sys

match = re.match(r"^v?(\d+)\.(\d+)\.(\d+)", sys.argv[1].strip())
if not match:
    raise SystemExit("System update lifecycle old version must start with a semantic version.")
print(f"{int(match.group(1)) + 1}.0.0")
PY
)"
NEW_VERSION="${ASTER_SYSTEM_UPDATE_E2E_NEW_VERSION:-${DEFAULT_NEW_VERSION}}"
python3 - "${OLD_VERSION}" "${NEW_VERSION}" <<'PY'
import re
import sys

def parts(value):
    match = re.match(r"^v?(\d+)\.(\d+)\.(\d+)", value.strip())
    if not match:
        raise SystemExit(f"Invalid system update lifecycle version: {value}")
    return tuple(map(int, match.groups()))

if parts(sys.argv[2]) <= parts(sys.argv[1]):
    raise SystemExit("System update lifecycle candidate version must be newer than the runtime version.")
PY
RUN_DIR_OWNED=0
if [ -n "${ASTER_SYSTEM_UPDATE_E2E_DIR:-}" ]; then
  RUN_DIR="${ASTER_SYSTEM_UPDATE_E2E_DIR}"
else
  RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/asterrouter-system-update-e2e.XXXXXX")"
  RUN_DIR_OWNED=1
fi
RUNTIME_DIR="${RUN_DIR}/runtime"
RUNTIME_BINARY="${RUNTIME_DIR}/asterrouter"
CANDIDATE_BINARY="${RUN_DIR}/candidate/asterrouter"
POSTGRES_DIR="${RUN_DIR}/postgres"
OFFICIAL_KEY_FILE="${RUN_DIR}/official-public-key"
GENERATION_FILE="${RUN_DIR}/generations.log"
SUPERVISOR_LOG="${RUN_DIR}/supervisor.log"
RUNTIME_LOG="${RUN_DIR}/runtime.log"
PIDS=()
POSTGRES_STARTED=0

if [ -d "${RUN_DIR}" ] && [ "${RUN_DIR_OWNED}" != "1" ] && find "${RUN_DIR}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "Refusing to overwrite non-empty lifecycle directory: ${RUN_DIR}" >&2
  exit 1
fi
mkdir -p \
  "${RUNTIME_DIR}" \
  "${RUN_DIR}/candidate" \
  "${RUN_DIR}/data" \
  "${RUN_DIR}/playwright"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required for the system update lifecycle E2E." >&2
    exit 1
  fi
}

require_free_port() {
  local name="$1"
  local port="$2"
  if lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "${name} port ${port} is already in use." >&2
    exit 1
  fi
}

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill -TERM "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" >/dev/null 2>&1 || true
  done
  if [ "${POSTGRES_STARTED}" = "1" ]; then
    "${PG_BINDIR}/pg_ctl" -D "${POSTGRES_DIR}" -m fast stop >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

for command in go npm lsof curl pg_config; do require_command "${command}"; done
PG_BINDIR="$(pg_config --bindir)"
for command in initdb pg_ctl createdb; do
  if [ ! -x "${PG_BINDIR}/${command}" ]; then
    echo "${PG_BINDIR}/${command} is required for the system update lifecycle E2E." >&2
    exit 1
  fi
done
if [ ! -d "${POSTGRES_SOCKET_DIR}" ] || [ ! -w "${POSTGRES_SOCKET_DIR}" ]; then
  echo "PostgreSQL socket directory must be writable: ${POSTGRES_SOCKET_DIR}" >&2
  exit 1
fi
require_free_port "Backend" "${BACKEND_PORT}"
require_free_port "Fake official" "${OFFICIAL_PORT}"
require_free_port "PostgreSQL" "${POSTGRES_PORT}"

build_binary() {
  local version="$1"
  local output="$2"
  (
    cd "${ROOT_DIR}/backend"
    CGO_ENABLED=0 go build -trimpath -ldflags \
      "-X github.com/astercloud/asterrouter/backend/internal/buildinfo.Version=${version} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Commit=e2e-system-update -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Date=2026-08-12T00:00:00Z -X github.com/astercloud/asterrouter/backend/internal/buildinfo.BuildType=release" \
      -o "${output}" ./cmd/asterrouter
  )
}

echo "Building isolated release binaries..."
build_binary "${OLD_VERSION}" "${RUNTIME_BINARY}"
build_binary "${NEW_VERSION}" "${CANDIDATE_BINARY}"
OLD_SHA="$(shasum -a 256 "${RUNTIME_BINARY}" | awk '{print $1}')"
NEW_SHA="$(shasum -a 256 "${CANDIDATE_BINARY}" | awk '{print $1}')"

echo "Building frontend assets..."
(
  cd "${ROOT_DIR}/frontend"
  npm run build
)

"${PG_BINDIR}/initdb" -D "${POSTGRES_DIR}" --auth=trust --username="${USER}" >/dev/null
"${PG_BINDIR}/pg_ctl" -D "${POSTGRES_DIR}" -l "${RUN_DIR}/postgres.log" \
  -o "-h 127.0.0.1 -p ${POSTGRES_PORT} -k ${POSTGRES_SOCKET_DIR}" start >/dev/null
POSTGRES_STARTED=1
"${PG_BINDIR}/createdb" -h 127.0.0.1 -p "${POSTGRES_PORT}" -U "${USER}" "${DATABASE_NAME}"
DATABASE_URL="postgresql://${USER}@127.0.0.1:${POSTGRES_PORT}/${DATABASE_NAME}?sslmode=disable"

(
  cd "${ROOT_DIR}"
  exec env \
    ASTER_E2E_OFFICIAL_PORT="${OFFICIAL_PORT}" \
    ASTER_E2E_OFFICIAL_KEY_FILE="${OFFICIAL_KEY_FILE}" \
    ASTER_E2E_OFFICIAL_CORE_RELEASE_FILE="${CANDIDATE_BINARY}" \
    ASTER_E2E_OFFICIAL_CORE_RELEASE_VERSION="${NEW_VERSION}" \
    node scripts/fake-official.mjs
) >"${RUN_DIR}/official.log" 2>&1 &
PIDS+=("$!")

for _ in $(seq 1 100); do
  [ -s "${OFFICIAL_KEY_FILE}" ] && break
  sleep 0.1
done
if [ ! -s "${OFFICIAL_KEY_FILE}" ]; then
  echo "Fake official did not publish trust material." >&2
  exit 1
fi
OFFICIAL_PUBLIC_KEY="$(tr -d '\r\n' < "${OFFICIAL_KEY_FILE}")"

(
  child_pid=""
  stop_supervisor() {
    if [ -n "${child_pid}" ]; then
      kill -TERM "${child_pid}" >/dev/null 2>&1 || true
      wait "${child_pid}" >/dev/null 2>&1 || true
    fi
    exit 0
  }
  trap stop_supervisor TERM INT
  generation=0
  while true; do
    generation=$((generation + 1))
    version_line="$("${RUNTIME_BINARY}" --version | head -1)"
    printf 'start %d %s\n' "${generation}" "${version_line}" >>"${GENERATION_FILE}"
    env \
      ASTERROUTER_SERVER_HTTP_LISTEN="127.0.0.1:${BACKEND_PORT}" \
      ASTERROUTER_SERVER_HTTP_FRONTEND_DIR="${ROOT_DIR}/frontend/dist" \
      ASTERROUTER_SERVER_BOOTSTRAP_DEMO_MODE=true \
      ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-system-update-e2e-secret \
      ASTERROUTER_SERVER_STORAGE_DATABASE_URL="${DATABASE_URL}" \
      ASTERROUTER_SERVER_OFFICIAL_CATALOG_MODE=online \
      ASTERROUTER_SERVER_OFFICIAL_CATALOG_URL="http://127.0.0.1:${OFFICIAL_PORT}/official/v1/catalog/index" \
      ASTERROUTER_SERVER_OFFICIAL_CATALOG_KEY_ID=aster-e2e-key-v1 \
      ASTERROUTER_SERVER_OFFICIAL_CATALOG_PUBLIC_KEY="${OFFICIAL_PUBLIC_KEY}" \
      ASTERROUTER_SERVER_MAINTENANCE_ALLOW_RESTART=true \
      ASTERROUTER_SERVER_PLUGINS_CACHE_DIR="${RUN_DIR}/data/plugin-cache" \
      ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR="${RUN_DIR}/data/plugin-active" \
      ASTERROUTER_SERVER_PLUGINS_DATA_DIR="${RUN_DIR}/data/plugin-data" \
      ASTERROUTER_SERVER_ARTIFACTS_DRIVER=local \
      ASTERROUTER_SERVER_ARTIFACTS_LOCAL_ROOT="${RUN_DIR}/data/artifacts" \
      ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR="${RUN_DIR}/data/backups" \
      ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR="${RUN_DIR}/data/diagnostics" \
      "${RUNTIME_BINARY}" server >>"${RUNTIME_LOG}" 2>&1 &
    child_pid="$!"
    set +e
    wait "${child_pid}"
    status="$?"
    set -e
    printf 'exit %d %d\n' "${generation}" "${status}" >>"${GENERATION_FILE}"
    child_pid=""
    sleep 0.1
  done
) >"${SUPERVISOR_LOG}" 2>&1 &
PIDS+=("$!")

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:${BACKEND_PORT}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
    break
  fi
  sleep 0.25
done
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/ready" | grep -q '"status":"ready"'

(
  cd "${ROOT_DIR}/frontend"
  ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${BACKEND_PORT}" \
    ASTER_E2E_OFFICIAL_URL="http://127.0.0.1:${OFFICIAL_PORT}" \
    ASTER_E2E_ARTIFACT_DIR="${RUN_DIR}/playwright" \
    ASTER_E2E_SYSTEM_UPDATE_LIFECYCLE=1 \
    ASTER_E2E_SYSTEM_UPDATE_RUNTIME_BINARY="${RUNTIME_BINARY}" \
    ASTER_E2E_SYSTEM_UPDATE_GENERATION_FILE="${GENERATION_FILE}" \
    ASTER_E2E_SYSTEM_UPDATE_OLD_SHA256="${OLD_SHA}" \
    ASTER_E2E_SYSTEM_UPDATE_NEW_SHA256="${NEW_SHA}" \
    ASTER_E2E_SYSTEM_UPDATE_OLD_VERSION="${OLD_VERSION}" \
    ASTER_E2E_SYSTEM_UPDATE_NEW_VERSION="${NEW_VERSION}" \
    npx playwright test e2e/system-maintenance.spec.ts \
      --grep @e2e-system-update-lifecycle-001 \
      --project chromium-desktop
)

sleep 0.5
START_COUNT="$(awk '$1 == "start" { count += 1 } END { print count + 0 }' "${GENERATION_FILE}")"
START_SEQUENCE="$(awk '$1 == "start" { print $NF }' "${GENERATION_FILE}" | paste -sd, -)"
EXIT_FAILURES="$(awk '$1 == "exit" && $3 != 0 { count += 1 } END { print count + 0 }' "${GENERATION_FILE}")"
if [ "${START_COUNT}" != "3" ]; then
  echo "System update lifecycle expected exactly three runtime generations, found ${START_COUNT}." >&2
  exit 1
fi
if [ "${START_SEQUENCE}" != "${OLD_VERSION},${NEW_VERSION},${OLD_VERSION}" ]; then
  echo "System update lifecycle runtime sequence was ${START_SEQUENCE}." >&2
  exit 1
fi
if [ "${EXIT_FAILURES}" != "0" ]; then
  echo "System update lifecycle observed a failed runtime generation." >&2
  exit 1
fi
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/ready" | grep -q '"status":"ready"'

{
  echo 'system_update_lifecycle=passed'
  echo 'journey=@e2e-system-update-lifecycle-001'
  echo "run_dir=${RUN_DIR}"
  echo "database=${DATABASE_NAME}"
  echo "postgres_port=${POSTGRES_PORT}"
  echo "backend_port=${BACKEND_PORT}"
  echo "official_port=${OFFICIAL_PORT}"
  echo "old_version=${OLD_VERSION}"
  echo "new_version=${NEW_VERSION}"
  echo "old_sha256=${OLD_SHA}"
  echo "new_sha256=${NEW_SHA}"
  echo 'generations=3'
  echo 'browser=chromium'
  echo 'database_class=dedicated_postgresql'
  echo 'execution=dedicated_release_binary_supervisor'
} >"${RUN_DIR}/report.txt"

echo "System update lifecycle passed. Evidence: ${RUN_DIR}/report.txt"
