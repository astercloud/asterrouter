#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACT_DIR="${ASTER_TEST_ARTIFACT_DIR:-${TMPDIR:-/tmp}/asterrouter-test-artifacts}"
BASELINE="${ASTER_BENCHMARK_BASELINE:-${ROOT_DIR}/docs/test/v1/performance-baseline.ubuntu-24.04.json}"

mkdir -p "${ARTIFACT_DIR}"
(
  cd "${ROOT_DIR}/backend"
  go test -run '^$' -bench '^BenchmarkGateway' -benchmem -count=5 ./internal/server | tee "${ARTIFACT_DIR}/gateway-benchmark.txt"
)
report_args=(
  --input "${ARTIFACT_DIR}/gateway-benchmark.txt"
  --baseline "${BASELINE}"
  --output "${ARTIFACT_DIR}/gateway-benchmark.json"
)
if [ "${ASTER_BENCHMARK_ENFORCE:-0}" = "1" ]; then
  report_args+=(--enforce)
fi
node "${ROOT_DIR}/scripts/benchmark-report.mjs" "${report_args[@]}"

echo "Benchmark artifacts: ${ARTIFACT_DIR}/gateway-benchmark.txt ${ARTIFACT_DIR}/gateway-benchmark.json"
