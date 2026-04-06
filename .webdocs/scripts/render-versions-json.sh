#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <gh-pages-dir>" >&2
  exit 1
fi

PAGES_DIR="$1"
python3 - "$PAGES_DIR" <<'PY'
import json
import os
import re
import sys

root = sys.argv[1]
entries = []

if os.path.isdir(os.path.join(root, "dev")):
    entries.append({"label": "dev", "path": "/dev/", "kind": "development"})
if os.path.isdir(os.path.join(root, "latest")):
    entries.append({"label": "latest", "path": "/latest/", "kind": "release-alias"})

versions = []
for name in os.listdir(root):
    if re.fullmatch(r"v\d+\.\d+\.\d+", name) and os.path.isdir(os.path.join(root, name)):
        versions.append(name)

def semver_key(version: str):
    major, minor, patch = version[1:].split(".")
    return (int(major), int(minor), int(patch))

for version in sorted(versions, key=semver_key, reverse=True):
    entries.append({"label": version, "path": f"/{version}/", "kind": "release"})

with open(os.path.join(root, "versions.json"), "w", encoding="utf-8") as fh:
    json.dump(entries, fh, indent=2)
    fh.write("\n")
PY

