#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(tr -d '\r\n' < "${ROOT_DIR}/backend/cmd/asterrouter/VERSION")}"
VERSION="${VERSION#v}"
DIST_DIR="${ASTER_DIST_DIR:-${ROOT_DIR}/dist}"
RUN_DIR="${ASTER_RELEASE_JOURNEY_DIR:-${TMPDIR:-/tmp}/asterrouter-release-journeys-$$}"
BACKEND_PORT="${ASTER_RELEASE_JOURNEY_PORT:-18087}"
UPSTREAM_PORT="${ASTER_RELEASE_JOURNEY_UPSTREAM_PORT:-19087}"
SMTP_PORT="${ASTER_RELEASE_JOURNEY_SMTP_PORT:-19088}"
MAIL_API_PORT="${ASTER_RELEASE_JOURNEY_MAIL_API_PORT:-19089}"
S3_PORT="${ASTER_RELEASE_JOURNEY_S3_PORT:-19090}"
S3_API_PORT="${ASTER_RELEASE_JOURNEY_S3_API_PORT:-19091}"
DATABASE_URL="${ASTER_RELEASE_TEST_DATABASE_URL:-}"
ADMIN_PASSWORD="release-browser-test-password"
COMMIT="${GITHUB_SHA:-$(git -C "${ROOT_DIR}" rev-parse HEAD 2>/dev/null || true)}"
COMMIT="${COMMIT:-unknown}"
PACKAGE_NAME="asterrouter_${VERSION}_linux_amd64"
ARCHIVE="${DIST_DIR}/${PACKAGE_NAME}.tar.gz"
PACKAGE_DIR="${RUN_DIR}/${PACKAGE_NAME}"
SMTP_DIR="${RUN_DIR}/fake-smtp"
SMTP_KEY="${SMTP_DIR}/server.key"
SMTP_CERT="${SMTP_DIR}/server.crt"
PIDS=()
RUNTIME_PID=""

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
  echo "Release browser journeys require Linux amd64." >&2
  exit 1
fi
if [ -z "${DATABASE_URL}" ]; then
  echo "ASTER_RELEASE_TEST_DATABASE_URL must point to a dedicated test database." >&2
  exit 1
fi
python3 - "${DATABASE_URL}" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
database = parsed.path.lstrip("/")
if parsed.scheme not in {"postgres", "postgresql"} or not parsed.hostname:
    raise SystemExit("Release journey database URL must use PostgreSQL.")
if database != "asterrouter_release_test" and not database.startswith("asterrouter_release_test_"):
    raise SystemExit("Release journey database URL must use the asterrouter_release_test prefix.")
PY
if [ ! -s "${ARCHIVE}" ]; then
  echo "Release archive is missing: ${ARCHIVE}" >&2
  exit 1
fi
if [ -d "${RUN_DIR}" ] && find "${RUN_DIR}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "Refusing to overwrite non-empty journey directory: ${RUN_DIR}" >&2
  exit 1
fi

require_free_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Required release journey port ${port} is already in use." >&2
    exit 1
  fi
}

database_url_for() {
  local suffix="$1"
  python3 - "${DATABASE_URL}" "${suffix}" <<'PY'
import sys
from urllib.parse import urlparse

parsed = urlparse(sys.argv[1])
database = parsed.path.lstrip("/")
print(parsed._replace(path="/" + database + "_" + sys.argv[2]).geturl())
PY
}

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill -TERM "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" >/dev/null 2>&1 || true
  done
}

start_runtime() {
  local port="$1"
  local database_url="$2"
  local journey_dir="$3"

  (
    cd "${PACKAGE_DIR}"
    exec env \
      "ASTERROUTER_SERVER_HTTP_LISTEN=127.0.0.1:${port}" \
      "ASTERROUTER_SERVER_HTTP_FRONTEND_DIR=${PACKAGE_DIR}/frontend/dist" \
      "ASTERROUTER_SERVER_SECURITY_ADMIN_PASSWORD=${ADMIN_PASSWORD}" \
      "ASTERROUTER_SERVER_SECURITY_SECRET_KEY=asterrouter-release-journey-test-secret" \
      "ASTERROUTER_SERVER_PLUGINS_CACHE_DIR=${journey_dir}/data/plugin-cache" \
      "ASTERROUTER_SERVER_PLUGINS_ACTIVE_DIR=${journey_dir}/data/plugin-active" \
      "ASTERROUTER_SERVER_ARTIFACTS_DRIVER=local" \
      "ASTERROUTER_SERVER_ARTIFACTS_LOCAL_ROOT=${journey_dir}/data/artifacts" \
      "ASTERROUTER_SERVER_MAINTENANCE_BACKUP_DIR=${journey_dir}/data/backups" \
      "ASTERROUTER_SERVER_MAINTENANCE_DIAGNOSTIC_DIR=${journey_dir}/data/diagnostics" \
      "ASTERROUTER_SERVER_STORAGE_DATABASE_URL=${database_url}" \
      "SSL_CERT_FILE=${SMTP_CERT}" \
      "AWS_CA_BUNDLE=${SMTP_CERT}" \
      ./asterrouter server
  ) >"${journey_dir}/runtime.log" 2>&1 &
  RUNTIME_PID="$!"
  PIDS+=("${RUNTIME_PID}")
}

wait_for_ready() {
  local pid="$1"
  local port="$2"
  for _ in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:${port}/ready" 2>/dev/null | grep -q '"status":"ready"'; then
      return 0
    fi
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      wait "${pid}" || true
      echo "Release candidate exited before becoming ready." >&2
      return 1
    fi
    sleep 0.25
  done
  curl -fsS "http://127.0.0.1:${port}/ready" | grep -q '"status":"ready"'
}

stop_runtime() {
  local pid="$1"
  local item
  local remaining=()
  kill -TERM "${pid}" >/dev/null 2>&1 || true
  wait "${pid}" >/dev/null 2>&1 || true
  for item in "${PIDS[@]}"; do
    if [ "${item}" != "${pid}" ]; then
      remaining+=("${item}")
    fi
  done
  PIDS=("${remaining[@]}")
}

run_enterprise_journey() {
  local grep_pattern="$1"
  local port="$2"
  local journey_dir="${RUN_DIR}/enterprise"
  mkdir -p "${journey_dir}"
  require_free_port "${port}"
  start_runtime "${port}" "${ENTERPRISE_DATABASE_URL}" "${journey_dir}"
  local pid="${RUNTIME_PID}"
  wait_for_ready "${pid}" "${port}"
  if ! curl -fsS "http://127.0.0.1:${port}/api/v1/setup/status" | grep -q '"setup_completed":true'; then
    echo "Enterprise journey database was not initialized by the setup journey." >&2
    return 1
  fi
  (
    cd "${ROOT_DIR}/frontend"
    CI=true \
      ASTER_E2E_EXTERNAL_URL="http://127.0.0.1:${port}" \
      ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" \
      ASTER_E2E_SMTP_PORT="${SMTP_PORT}" \
      ASTER_E2E_MAIL_API_URL="http://127.0.0.1:${MAIL_API_PORT}" \
      ASTER_E2E_S3_PORT="${S3_PORT}" \
      ASTER_E2E_S3_API_URL="http://127.0.0.1:${S3_API_PORT}" \
      ASTER_E2E_ARTIFACT_DIR="${journey_dir}/playwright" \
      ASTER_E2E_USERNAME=admin \
      ASTER_E2E_PASSWORD="${ADMIN_PASSWORD}" \
      ASTER_E2E_EXPECT_DEMO_MODE=false \
      ASTER_E2E_POSTGRES_AVAILABLE=1 \
      ASTER_E2E_ALLOW_DESTRUCTIVE_RESTORE=1 \
      ASTER_E2E_DATABASE_NAME="$(python3 - "${database_url}" <<'PY'
import sys
from urllib.parse import urlparse
print(urlparse(sys.argv[1]).path.lstrip('/'))
PY
)" \
      npx playwright test --grep "${grep_pattern}"
  )
  stop_runtime "${pid}"
}

require_free_port "${BACKEND_PORT}"
require_free_port "${UPSTREAM_PORT}"
require_free_port "${SMTP_PORT}"
require_free_port "${MAIL_API_PORT}"
require_free_port "${S3_PORT}"
require_free_port "${S3_API_PORT}"
trap cleanup EXIT INT TERM

mkdir -p "${RUN_DIR}"
tar -C "${RUN_DIR}" -xzf "${ARCHIVE}"
node "${ROOT_DIR}/frontend/scripts/check-e2e-coverage.mjs"
mkdir -p "${SMTP_DIR}"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
  -keyout "${SMTP_KEY}" \
  -out "${SMTP_CERT}" >/dev/null 2>&1

(
  cd "${ROOT_DIR}"
  ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" node "scripts/fake-openai.mjs"
) >"${RUN_DIR}/fake-upstream.log" 2>&1 &
PIDS+=("$!")

(
  cd "${ROOT_DIR}"
  ASTER_E2E_SMTP_PORT="${SMTP_PORT}" \
    ASTER_E2E_MAIL_API_PORT="${MAIL_API_PORT}" \
    ASTER_E2E_SMTP_KEY="${SMTP_KEY}" \
    ASTER_E2E_SMTP_CERT="${SMTP_CERT}" \
    node "scripts/fake-smtp.mjs"
) >"${RUN_DIR}/fake-smtp.log" 2>&1 &
PIDS+=("$!")

(
  cd "${ROOT_DIR}"
  ASTER_E2E_S3_PORT="${S3_PORT}" \
    ASTER_E2E_S3_API_PORT="${S3_API_PORT}" \
    ASTER_E2E_S3_KEY="${SMTP_KEY}" \
    ASTER_E2E_S3_CERT="${SMTP_CERT}" \
    node "scripts/fake-s3.mjs"
) >"${RUN_DIR}/fake-s3.log" 2>&1 &
PIDS+=("$!")

ENTERPRISE_DATABASE_URL="$(database_url_for enterprise)"
ASTER_SETUP_JOURNEY_DATABASE_URL="${ENTERPRISE_DATABASE_URL}" \
  ASTER_SETUP_JOURNEY_DIR="${RUN_DIR}/setup" \
  ASTER_SETUP_JOURNEY_PORT="${BACKEND_PORT}" \
  ASTER_SETUP_JOURNEY_BINARY="${PACKAGE_DIR}/asterrouter" \
  ASTER_SETUP_JOURNEY_ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
  bash "${ROOT_DIR}/scripts/test-setup-browser-journey.sh"

RELEASE_GREP_PATTERN="$(node "${ROOT_DIR}/frontend/scripts/run-e2e-gate.mjs" release --exclude-kind setup --exclude-id @e2e-system-update-lifecycle-001 --print-pattern)"
RELEASE_SCENARIO_IDS="$(node "${ROOT_DIR}/frontend/scripts/run-e2e-gate.mjs" release --exclude-kind setup --exclude-id @e2e-system-update-lifecycle-001 --print-ids)"
run_enterprise_journey "${RELEASE_GREP_PATTERN}" "$((BACKEND_PORT + 1))"

{
  echo 'release_browser_journeys=passed'
  echo "version=${VERSION}"
  echo "commit=${COMMIT}"
  echo 'platform=linux/amd64'
  echo 'execution=candidate_archive'
  echo "candidate=${ARCHIVE}"
  echo "tested_url=http://127.0.0.1:$((BACKEND_PORT + 1))"
  echo 'database_class=dedicated_postgresql'
  echo 'product=enterprise'
  echo 'isolation=dedicated_postgresql_database_and_runtime'
  echo "journeys=${RELEASE_SCENARIO_IDS}"
  echo 'first_install=enterprise'
  echo 'browser=chromium'
} >"${RUN_DIR}/report.txt"

echo "Release browser journeys passed. Evidence: ${RUN_DIR}/report.txt"
