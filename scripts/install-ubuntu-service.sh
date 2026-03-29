#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run as root: sudo ./scripts/install-ubuntu-service.sh"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required but was not found. Install Go 1.22+ first."
  exit 1
fi

SERVICE_NAME="tordown"
RUN_USER="${SUDO_USER:-$(whoami)}"
RUN_GROUP="$(id -gn "${RUN_USER}")"
ENV_FILE="/etc/tordown.env"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
BIN_DIR="${APP_DIR}/bin"
BIN_PATH="${BIN_DIR}/tordown"

get_env_value() {
  local key="$1"
  local fallback="$2"

  if [[ -f "${ENV_FILE}" ]]; then
    local existing
    existing="$(grep -E "^${key}=" "${ENV_FILE}" | tail -n 1 | cut -d'=' -f2- || true)"
    if [[ -n "${existing}" ]]; then
      printf '%s' "${existing}"
      return
    fi
  fi

  printf '%s' "${fallback}"
}

upsert_env_value() {
  local key="$1"
  local value="$2"

  if grep -q "^${key}=" "${ENV_FILE}"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "${ENV_FILE}"
  else
    echo "${key}=${value}" >> "${ENV_FILE}"
  fi
}

LISTEN_ADDR="${TORDOWN_LISTEN_ADDR:-$(get_env_value TORDOWN_LISTEN_ADDR :8080)}"
DOWNLOAD_DIR="${TORDOWN_DOWNLOAD_DIR:-$(get_env_value TORDOWN_DOWNLOAD_DIR /var/lib/tordown/downloads)}"
SSL_CERT="${TORDOWN_SSL_CERT:-$(get_env_value TORDOWN_SSL_CERT "")}"
SSL_KEY="${TORDOWN_SSL_KEY:-$(get_env_value TORDOWN_SSL_KEY "")}"
DOMAIN="${TORDOWN_DOMAIN:-$(get_env_value TORDOWN_DOMAIN "")}"

mkdir -p "${BIN_DIR}" "${DOWNLOAD_DIR}"
chown -R "${RUN_USER}:${RUN_GROUP}" "${DOWNLOAD_DIR}"

cd "${APP_DIR}"
go mod tidy
go build -o "${BIN_PATH}" ./cmd/server
chown "${RUN_USER}:${RUN_GROUP}" "${BIN_PATH}"
chmod 0755 "${BIN_PATH}"

if [[ ! -f "${ENV_FILE}" ]]; then
  cat > "${ENV_FILE}" <<EOF
TORDOWN_LISTEN_ADDR=${LISTEN_ADDR}
TORDOWN_DOWNLOAD_DIR=${DOWNLOAD_DIR}
# Optional TLS settings:
# TORDOWN_SSL_CERT=/etc/letsencrypt/live/example.com/fullchain.pem
# TORDOWN_SSL_KEY=/etc/letsencrypt/live/example.com/privkey.pem
# TORDOWN_DOMAIN=example.duckdns.org
EOF
fi

upsert_env_value "TORDOWN_LISTEN_ADDR" "${LISTEN_ADDR}"
upsert_env_value "TORDOWN_DOWNLOAD_DIR" "${DOWNLOAD_DIR}"

if [[ -n "${SSL_CERT}" ]]; then
  upsert_env_value "TORDOWN_SSL_CERT" "${SSL_CERT}"
fi

if [[ -n "${SSL_KEY}" ]]; then
  upsert_env_value "TORDOWN_SSL_KEY" "${SSL_KEY}"
fi

if [[ -n "${DOMAIN}" ]]; then
  upsert_env_value "TORDOWN_DOMAIN" "${DOMAIN}"
fi

chmod 0644 "${ENV_FILE}"

cat > "${UNIT_FILE}" <<EOF
[Unit]
Description=TorDown Torrent Web Manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_GROUP}
WorkingDirectory=${APP_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${BIN_PATH}
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "${SERVICE_NAME}"

systemctl --no-pager --full status "${SERVICE_NAME}" | head -n 20

echo
echo "Installed and started ${SERVICE_NAME}."
echo "Update settings in ${ENV_FILE}, then run: sudo systemctl restart ${SERVICE_NAME}"
