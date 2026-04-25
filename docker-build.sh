#!/usr/bin/env bash
# Build and deploy CLIProxyAPI from the current source tree.
# This script injects version metadata into the Docker build so the
# management UI can display the server version and build time correctly.

set -euo pipefail

VERSION="$(git describe --tags --always --dirty)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

export VERSION
export COMMIT
export BUILD_DATE

echo "Building and deploying CLIProxyAPI with:"
echo "  Version:    ${VERSION}"
echo "  Commit:     ${COMMIT}"
echo "  Build Date: ${BUILD_DATE}"
echo "----------------------------------------"

docker compose up --build -d --remove-orphans

echo "Deployment started."
echo "Run 'docker compose logs -f' to see the logs."
