#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPSTREAM_PORT="${ASTER_E2E_UPSTREAM_PORT:-29000}"
SMTP_PORT="${ASTER_E2E_SMTP_PORT:-29001}"
MAIL_API_PORT="${ASTER_E2E_MAIL_API_PORT:-29002}"
S3_PORT="${ASTER_E2E_S3_PORT:-29003}"
S3_API_PORT="${ASTER_E2E_S3_API_PORT:-29004}"
OIDC_PORT="${ASTER_E2E_OIDC_PORT:-29005}"
OFFICIAL_PORT="${ASTER_E2E_OFFICIAL_PORT:-29006}"
RUN_DIR_OWNED=0
if [ -n "${ASTER_E2E_RUN_DIR:-}" ]; then
  RUN_DIR="${ASTER_E2E_RUN_DIR}"
else
  RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/asterrouter-e2e.XXXXXX")"
  RUN_DIR_OWNED=1
fi
SMTP_KEY="${RUN_DIR}/fake-smtp.key"
SMTP_CERT="${RUN_DIR}/fake-smtp.crt"
OIDC_KEY="${RUN_DIR}/fake-oidc.key"
OIDC_CERT="${RUN_DIR}/fake-oidc.crt"
OIDC_READY_FILE="${RUN_DIR}/fake-oidc.ready"
OIDC_ENABLED="${ASTER_E2E_OIDC_ENABLED:-1}"
OFFICIAL_KEY_FILE="${RUN_DIR}/fake-official-public-key"
PIDS=()

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill -TERM "${pid}" >/dev/null 2>&1 || true
  done
  for pid in "${PIDS[@]}"; do
    wait "${pid}" >/dev/null 2>&1 || true
  done
  if [ "${RUN_DIR_OWNED}" = "1" ] && [ "${ASTER_E2E_KEEP_RUN_DIR:-0}" != "1" ]; then
    rm -rf -- "${RUN_DIR}"
  fi
}

trap cleanup EXIT INT TERM

require_free_port() {
  local name="$1"
  local port="$2"
  if command -v lsof >/dev/null 2>&1 && lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "${name} port ${port} is already in use." >&2
    exit 1
  fi
}

require_free_port "Fake upstream" "${UPSTREAM_PORT}"
require_free_port "Fake SMTP" "${SMTP_PORT}"
require_free_port "Fake SMTP mailbox API" "${MAIL_API_PORT}"
require_free_port "Fake S3" "${S3_PORT}"
require_free_port "Fake S3 API" "${S3_API_PORT}"
if [ "${OIDC_ENABLED}" = "1" ]; then
  require_free_port "Fake OIDC" "${OIDC_PORT}"
fi
require_free_port "Fake official services" "${OFFICIAL_PORT}"

if ! command -v openssl >/dev/null 2>&1; then
  echo "OpenSSL is required to create the isolated fake SMTP certificate." >&2
  exit 1
fi

mkdir -p "${RUN_DIR}"
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
  -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
  -keyout "${SMTP_KEY}" \
  -out "${SMTP_CERT}" >/dev/null 2>&1
if [ "${OIDC_ENABLED}" = "1" ]; then
  cp "${SMTP_KEY}" "${OIDC_KEY}"
  cp "${SMTP_CERT}" "${OIDC_CERT}"
  rm -f -- "${OIDC_READY_FILE}"
fi

(
  cd "${ROOT_DIR}"
  ASTER_E2E_UPSTREAM_PORT="${UPSTREAM_PORT}" node "scripts/fake-openai.mjs"
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}"
  ASTER_E2E_OFFICIAL_PORT="${OFFICIAL_PORT}" \
    ASTER_E2E_OFFICIAL_KEY_FILE="${OFFICIAL_KEY_FILE}" \
    node "scripts/fake-official.mjs"
) &
PIDS+=("$!")

for _ in {1..100}; do
  if [ -s "${OFFICIAL_KEY_FILE}" ]; then
    break
  fi
  sleep 0.1
done
if [ ! -s "${OFFICIAL_KEY_FILE}" ]; then
  echo "Fake official services did not publish signing trust material." >&2
  exit 1
fi
OFFICIAL_PUBLIC_KEY="$(tr -d '\r\n' < "${OFFICIAL_KEY_FILE}")"

if [ "${OIDC_ENABLED}" = "1" ]; then
  (
    cd "${ROOT_DIR}"
    ASTER_E2E_OIDC_PORT="${OIDC_PORT}" \
      ASTER_DEV_FRONTEND_PORT="${ASTER_DEV_FRONTEND_PORT:-5173}" \
      ASTER_E2E_OIDC_KEY="${OIDC_KEY}" \
      ASTER_E2E_OIDC_CERT="${OIDC_CERT}" \
      ASTER_E2E_OIDC_READY_FILE="${OIDC_READY_FILE}" \
      node "scripts/fake-oidc.mjs"
  ) &
  PIDS+=("$!")
fi

(
  cd "${ROOT_DIR}"
  ASTER_E2E_SMTP_PORT="${SMTP_PORT}" \
    ASTER_E2E_MAIL_API_PORT="${MAIL_API_PORT}" \
    ASTER_E2E_SMTP_KEY="${SMTP_KEY}" \
    ASTER_E2E_SMTP_CERT="${SMTP_CERT}" \
    node "scripts/fake-smtp.mjs"
) &
PIDS+=("$!")

if [ "${OIDC_ENABLED}" = "1" ]; then
  for _ in {1..100}; do
    if [ -s "${OIDC_READY_FILE}" ]; then
      break
    fi
    sleep 0.1
  done
  if [ ! -s "${OIDC_READY_FILE}" ]; then
    echo "Fake OIDC did not become ready for discovery." >&2
    exit 1
  fi
fi

(
  cd "${ROOT_DIR}"
  ASTER_E2E_S3_PORT="${S3_PORT}" \
    ASTER_E2E_S3_API_PORT="${S3_API_PORT}" \
    ASTER_E2E_S3_KEY="${SMTP_KEY}" \
    ASTER_E2E_S3_CERT="${SMTP_CERT}" \
    node "scripts/fake-s3.mjs"
) &
PIDS+=("$!")

(
  cd "${ROOT_DIR}"
  ASTER_DEV_BACKEND_SSL_CERT_FILE="${SMTP_CERT}" \
    ASTER_DEV_SKIP_ENV_FILE=1 \
    ASTER_E2E_OIDC_ENABLED="${OIDC_ENABLED}" \
    ASTER_E2E_OIDC_PORT="${OIDC_PORT}" \
    ASTER_E2E_OIDC_CLIENT_ID=asterrouter-e2e \
    ASTER_E2E_OIDC_CLIENT_SECRET=asterrouter-e2e-secret \
    ASTERROUTER_SERVER_OFFICIAL_CATALOG_MODE=online \
    ASTERROUTER_SERVER_OFFICIAL_CATALOG_URL="http://127.0.0.1:${OFFICIAL_PORT}/official/v1/catalog/index" \
    ASTERROUTER_SERVER_OFFICIAL_CATALOG_SERVICES_URL="http://127.0.0.1:${OFFICIAL_PORT}/official/v1/services" \
    ASTERROUTER_SERVER_OFFICIAL_CATALOG_KEY_ID=aster-e2e-key-v1 \
    ASTERROUTER_SERVER_OFFICIAL_CATALOG_PUBLIC_KEY="${OFFICIAL_PUBLIC_KEY}" \
    ASTERROUTER_SERVER_OFFICIAL_LICENSE_URL="http://127.0.0.1:${OFFICIAL_PORT}/official/v1" \
    ASTERROUTER_SERVER_OFFICIAL_LICENSE_REDEEM_URL="http://127.0.0.1:${OFFICIAL_PORT}/official/v1" \
    ASTERROUTER_SERVER_OFFICIAL_LICENSE_KEY_ID=aster-e2e-key-v1 \
    ASTERROUTER_SERVER_OFFICIAL_LICENSE_PUBLIC_KEY="${OFFICIAL_PUBLIC_KEY}" \
    ASTERROUTER_SERVER_OFFICIAL_INSTANCE_ID=inst_e2e_browser \
    ASTERROUTER_SERVER_OFFICIAL_INSTANCE_FINGERPRINT=sha256:e2e-browser-fingerprint \
    ASTERROUTER_SERVER_OFFICIAL_INSTANCE_DISPLAY_NAME="E2E Browser Router" \
    bash "scripts/dev.sh" --no-kill-occupied
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
