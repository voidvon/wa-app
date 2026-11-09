#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

if [[ $# -gt 1 ]]; then
  echo "Usage: scripts/build-binary.sh [output-file]" >&2
  exit 1
fi

output="${1:-dist/wa-app}"
build_version="${VERSION:-$(git describe --tags --always --dirty)}"
scripts/generate-proto.sh
npm --prefix webui ci --no-audit --no-fund
npm --prefix webui run build
mkdir -p "$(dirname "${output}")"
CGO_ENABLED=0 go build -tags dashboard -trimpath \
  -ldflags "-s -w -X main.version=${build_version}" -o "${output}" ./cmd/wa-app
echo "Built ${output}"
