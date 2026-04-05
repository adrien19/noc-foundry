#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOPO_FILE="${TOPO_FILE:-${ROOT_DIR}/containerlab/noc-foundry-lab.clab.yaml}"

usage() {
  cat <<'EOF'
Usage:
  examples/containerlab/manage-containerlab.sh deploy
  examples/containerlab/manage-containerlab.sh destroy
  examples/containerlab/manage-containerlab.sh inspect
  examples/containerlab/manage-containerlab.sh redeploy

Environment:
  TOPO_FILE   Optional topology file path (default: examples/containerlab/noc-foundry-lab.clab.yaml)
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_cmd containerlab

if [[ ! -f "${TOPO_FILE}" ]]; then
  echo "topology file not found: ${TOPO_FILE}" >&2
  exit 1
fi

cmd="${1:-}"
case "${cmd}" in
  deploy)
    containerlab deploy -t "${TOPO_FILE}"
    containerlab inspect -t "${TOPO_FILE}"
    ;;
  destroy)
    containerlab destroy -t "${TOPO_FILE}" --cleanup
    ;;
  inspect)
    containerlab inspect -t "${TOPO_FILE}"
    ;;
  redeploy)
    containerlab destroy -t "${TOPO_FILE}" --cleanup || true
    containerlab deploy -t "${TOPO_FILE}"
    containerlab inspect -t "${TOPO_FILE}"
    ;;
  *)
    usage
    exit 1
    ;;
esac
