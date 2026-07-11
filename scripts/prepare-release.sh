#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-}"

usage() {
  cat <<'EOF'
Usage:
  scripts/prepare-release.sh v0.1.2
  scripts/prepare-release.sh 0.1.2

Updates release version sources before creating a tag.
EOF
}

if [ -z "$VERSION" ] || [ "$VERSION" = "-h" ] || [ "$VERSION" = "--help" ]; then
  usage
  exit 1
fi

VERSION="${VERSION#v}"
if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$'; then
  echo "invalid semantic version: ${VERSION}" >&2
  exit 1
fi

printf '%s\n' "$VERSION" > "${ROOT_DIR}/backend/cmd/asterrouter/VERSION"

node - "$ROOT_DIR/frontend/package.json" "$VERSION" <<'NODE'
const fs = require('node:fs')
const [path, version] = process.argv.slice(2)
const data = JSON.parse(fs.readFileSync(path, 'utf8'))
data.version = version
fs.writeFileSync(path, `${JSON.stringify(data, null, 2)}\n`)
NODE

cat <<EOF
Prepared AsterRouter ${VERSION}.

Next steps:
  git diff -- backend/cmd/asterrouter/VERSION frontend/package.json
  git commit -am "chore: prepare release v${VERSION}"
  git tag -a "v${VERSION}" -m "AsterRouter ${VERSION}"
  git push origin main "v${VERSION}"
EOF
