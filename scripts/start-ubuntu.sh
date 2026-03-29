#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required but was not found. Install Go 1.22+ first."
  exit 1
fi

LISTEN_ADDR="${TORDOWN_LISTEN_ADDR:-:8080}"
DOWNLOAD_DIR="${TORDOWN_DOWNLOAD_DIR:-${APP_DIR}/downloads}"

mkdir -p "${APP_DIR}/bin" "${DOWNLOAD_DIR}"

cd "${APP_DIR}"
go mod tidy
go build -o "${APP_DIR}/bin/tordown" ./cmd/server

echo "Starting TorDown on ${LISTEN_ADDR}"
TORDOWN_LISTEN_ADDR="${LISTEN_ADDR}" \
TORDOWN_DOWNLOAD_DIR="${DOWNLOAD_DIR}" \
exec "${APP_DIR}/bin/tordown"
