#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_HA_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-ha-test-artifacts}"
PORT="${ASTER_HA_TEST_PORT:-18081}"
VERSION="${ASTER_HA_TEST_VERSION:-ha-container-test}"
RUN_ID="${GITHUB_RUN_ID:-local}-$$"
PROJECT="asterrouter-ha-test-${RUN_ID}"
NETWORK="${PROJECT}_default"
POSTGRES_CONTAINER="${PROJECT}-postgres"
IMAGE="${ASTER_HA_TEST_IMAGE:-asterrouter:container-test}"
COMPOSE_FILE="${ROOT_DIR}/deploy/docker-compose.ha.yml"
POSTGRES_VERSION=""

mkdir -p "${ARTIFACT_DIR}"
printf '' >"${ARTIFACT_DIR}/report.txt"

compose() {
  docker compose --project-name "${PROJECT}" --file "${COMPOSE_FILE}" "$@"
}

service_container() {
  compose ps --all --quiet "$1"
}

capture_evidence() {
  compose logs --no-color >"${ARTIFACT_DIR}/ha-compose.log" 2>&1 || true
  docker logs "${POSTGRES_CONTAINER}" >"${ARTIFACT_DIR}/ha-postgres.log" 2>&1 || true
  compose ps --all >"${ARTIFACT_DIR}/ha-compose-ps.txt" 2>&1 || true
}

cleanup() {
  capture_evidence
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  docker rm --force "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

wait_for_postgres() {
  local consecutive_successes=0
  local detected_version=""
  for _ in $(seq 1 60); do
    if detected_version="$(docker exec "${POSTGRES_CONTAINER}" \
      psql --username=asterrouter --dbname=asterrouter_ha_test \
      --tuples-only --no-align --command='SHOW server_version' 2>/dev/null)"; then
      consecutive_successes=$((consecutive_successes + 1))
      if [ "${consecutive_successes}" -ge 3 ]; then
        POSTGRES_VERSION="${detected_version}"
        return 0
      fi
    else
      consecutive_successes=0
    fi
    sleep 1
  done
  docker logs "${POSTGRES_CONTAINER}" >&2 || true
  return 1
}

wait_for_healthy_service() {
  local service="$1"
  local container_id
  local status
  container_id="$(service_container "${service}")"
  test -n "${container_id}"
  for _ in $(seq 1 120); do
    status="$(docker inspect "${container_id}" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}')"
    if [ "${status}" = "healthy" ]; then
      return 0
    fi
    if [ "${status}" = "exited" ] || [ "${status}" = "dead" ]; then
      docker logs "${container_id}" >&2 || true
      return 1
    fi
    sleep 1
  done
  docker inspect "${container_id}" --format '{{json .State}}' >&2
  return 1
}

assert_proxy_available() {
  local phase="$1"
  for _ in $(seq 1 20); do
    curl --fail --silent --show-error "http://127.0.0.1:${PORT}/health" | grep -Fq '"status":"ok"'
    curl --fail --silent --show-error "http://127.0.0.1:${PORT}/ready" | grep -Fq '"status":"ready"'
  done
  curl --fail --silent --show-error "http://127.0.0.1:${PORT}/console/overview" | grep -Fq '<div id="app"></div>'
  printf '%s=passed\n' "${phase}" >>"${ARTIFACT_DIR}/report.txt"
}

if ! docker image inspect "${IMAGE}" >/dev/null 2>&1; then
  docker build \
    --build-arg ASTER_VERSION="${VERSION}" \
    --build-arg ASTER_BUILD_TYPE=release \
    --tag "${IMAGE}" "${ROOT_DIR}"
fi
test "$(docker image inspect "${IMAGE}" --format '{{.Config.User}}')" = "asterrouter"

export ASTERROUTER_IMAGE="${IMAGE}"
export ASTERROUTER_DATABASE_URL='postgres://asterrouter:asterrouter@postgres:5432/asterrouter_ha_test?sslmode=disable'
export ASTERROUTER_SECRET_KEY='asterrouter-ha-container-test-secret'
export ASTERROUTER_ADMIN_PASSWORD='asterrouter-ha-container-test-password'
export ASTERROUTER_DEMO_MODE='false'
export ASTERROUTER_HA_BIND_ADDRESS='127.0.0.1'
export ASTERROUTER_HA_PORT="${PORT}"

compose config --quiet
docker network create \
  --label "com.docker.compose.project=${PROJECT}" \
  --label 'com.docker.compose.network=default' \
  "${NETWORK}" >/dev/null
docker run --detach --name "${POSTGRES_CONTAINER}" --network "${NETWORK}" --network-alias postgres \
  --env POSTGRES_DB=asterrouter_ha_test \
  --env POSTGRES_USER=asterrouter \
  --env POSTGRES_PASSWORD=asterrouter \
  postgres:16-alpine >/dev/null
wait_for_postgres
case "${POSTGRES_VERSION}" in
  16.*) ;;
  *)
    echo "HA container acceptance requires PostgreSQL 16, got ${POSTGRES_VERSION}." >&2
    exit 1
    ;;
esac

compose up --detach --no-build
wait_for_healthy_service asterrouter-a
wait_for_healthy_service asterrouter-b
wait_for_healthy_service proxy
assert_proxy_available dual_instances

compose stop --timeout 15 asterrouter-a >/dev/null
test "$(docker inspect "$(service_container asterrouter-a)" --format '{{.State.ExitCode}}')" = "0"
assert_proxy_available instance_a_stopped

compose start asterrouter-a >/dev/null
wait_for_healthy_service asterrouter-a
compose stop --timeout 15 asterrouter-b >/dev/null
test "$(docker inspect "$(service_container asterrouter-b)" --format '{{.State.ExitCode}}')" = "0"
assert_proxy_available instance_b_stopped

compose stop --timeout 15 asterrouter-a >/dev/null
test "$(docker inspect "$(service_container asterrouter-a)" --format '{{.State.ExitCode}}')" = "0"
if curl --fail --silent --show-error --max-time 10 "http://127.0.0.1:${PORT}/ready" >/dev/null 2>&1; then
  echo "HA proxy reported readiness after both application instances stopped." >&2
  exit 1
fi

{
  echo 'ha_container_acceptance=passed'
  echo "postgresql_version=${POSTGRES_VERSION}"
  echo 'application_instances=2'
  echo 'proxy=nginx'
  echo 'single_instance_failover=passed'
  echo 'all_instances_stopped=unavailable'
} >>"${ARTIFACT_DIR}/report.txt"

echo "HA container acceptance passed on port ${PORT}."
