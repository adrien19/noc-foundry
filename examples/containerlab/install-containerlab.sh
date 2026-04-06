#!/bin/bash

set -euo pipefail

if command -v containerlab >/dev/null 2>&1; then
    echo "Containerlab is already installed."
    containerlab version 2>/dev/null | head -n 1 || true
    exit 0
fi

echo "Installing containerlab for local topology labs..."
export SETUP_SSHD="false"
curl -sL https://containerlab.dev/setup | sudo -E bash -s "all"

echo "Containerlab installed."
containerlab version 2>/dev/null | head -n 1 || true
