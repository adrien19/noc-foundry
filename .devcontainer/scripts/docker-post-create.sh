#!/bin/bash

# Install Node.js dependencies

# Check if Node.js is installed
if ! command -v node --version &> /dev/null
then
    echo "Node.js could not be found, installing Node.js"
    curl -fsSL https://deb.nodesource.com/setup_24.x | sudo bash -
    sudo apt install nodejs -y
fi
# Now check node and npm versions
echo "Node.js version: $(node --version)"
echo "npm version: $(npm --version)"
# install pnpm if not installed
if ! command -v pnpm --version &> /dev/null
then
    echo "pnpm could not be found, installing pnpm"
    sudo npm install -g pnpm
fi
# Check pnpm version
echo "pnpm version: $(pnpm --version)"

# Install Hugo for webdocs
if ! command -v hugo version &> /dev/null
then
    HUGO_VERSION=0.146.0
    ARCH="$(dpkg --print-architecture)"
    case "$ARCH" in
    amd64) HUGO_ARCH="amd64" ;;
    arm64) HUGO_ARCH="arm64" ;;
    *) echo "Unsupported arch: $ARCH"; exit 1 ;;
    esac

    curl -fsSL -o /tmp/hugo.deb \
    "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${HUGO_ARCH}.deb"
    sudo apt install -y /tmp/hugo.deb
    rm -f /tmp/hugo.deb
fi
# Check Hugo version
echo "Hugo version: $(hugo version)"
if command -v containerlab &> /dev/null
then
    echo "Containerlab version: $(containerlab version 2>/dev/null | head -n 1)"
else
    echo "Containerlab is not installed by default."
    echo "Use examples/containerlab/install-containerlab.sh if you want the local SR Linux lab."
fi
