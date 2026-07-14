#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR="${1:?usage: scripts/collect-flaky-history.sh <output-directory>}"
REPOSITORY="${GITHUB_REPOSITORY:-}"

mkdir -p "${OUTPUT_DIR}"
if [ -z "${REPOSITORY}" ]; then
  echo "GITHUB_REPOSITORY is not set; no remote flaky history collected." >&2
  exit 0
fi
if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI is unavailable; no remote flaky history collected." >&2
  exit 0
fi

for run_id in $(gh run list --repo "${REPOSITORY}" --workflow "Nightly Tests" --status completed --limit 20 --json databaseId --jq '.[].databaseId'); do
  destination="${OUTPUT_DIR}/${run_id}"
  mkdir -p "${destination}"
  gh run download "${run_id}" --repo "${REPOSITORY}" --name test-health-report --dir "${destination}" >/dev/null 2>&1 || true
done
