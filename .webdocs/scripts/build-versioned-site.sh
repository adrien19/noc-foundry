#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <base-url> <output-dir>" >&2
  exit 1
fi

BASE_URL="$1"
OUTPUT_DIR="$2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCS_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

rm -rf "${OUTPUT_DIR}"
mkdir -p "${OUTPUT_DIR}"

hugo \
  --source "${DOCS_DIR}" \
  --environment production \
  --gc \
  --minify \
  --baseURL "${BASE_URL}" \
  --destination "${OUTPUT_DIR}"

