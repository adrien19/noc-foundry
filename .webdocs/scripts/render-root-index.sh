#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <gh-pages-dir>" >&2
  exit 1
fi

PAGES_DIR="$1"
TARGET="/dev/"
if [[ -d "${PAGES_DIR}/latest" ]]; then
  TARGET="/latest/"
fi

cat > "${PAGES_DIR}/index.html" <<EOF
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>NOCFoundry Docs</title>
    <meta http-equiv="refresh" content="0; url=${TARGET}" />
    <link rel="canonical" href="${TARGET}" />
  </head>
  <body>
    <p>Redirecting to <a href="${TARGET}">${TARGET}</a>…</p>
    <script>window.location.replace(${TARGET@Q});</script>
  </body>
</html>
EOF

