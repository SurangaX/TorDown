#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run as root: sudo ./scripts/install-ubuntu.sh"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SERVICE_INSTALLER="${SCRIPT_DIR}/install-ubuntu-service.sh"
SSL_SETUP_SCRIPT="${APP_DIR}/setup-ssl.sh"

if [[ ! -x "${SERVICE_INSTALLER}" ]]; then
  chmod +x "${SERVICE_INSTALLER}"
fi

if [[ ! -f "${SSL_SETUP_SCRIPT}" ]]; then
  echo "Missing setup script: ${SSL_SETUP_SCRIPT}"
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required but was not found. Install Go 1.22+ first."
  exit 1
fi

prompt_default() {
  local prompt="$1"
  local default_value="$2"
  local input_value

  read -r -p "${prompt} [${default_value}]: " input_value
  if [[ -z "${input_value}" ]]; then
    printf '%s' "${default_value}"
  else
    printf '%s' "${input_value}"
  fi
}

echo "=== TorDown Ubuntu Installer ==="
echo "This script installs the service and can optionally configure SSL."
echo ""

download_dir="$(prompt_default "Download directory" "/var/lib/tordown/downloads")"

echo ""
echo "Choose install mode:"
echo "  1) HTTP only (default, listen on :8080)"
echo "  2) HTTPS with Let's Encrypt (listen on :443)"
read -r -p "Enter choice [1/2]: " install_mode
install_mode="${install_mode:-1}"

case "${install_mode}" in
  1)
    listen_addr="$(prompt_default "Listen address" ":8080")"
    echo ""
    echo "Installing TorDown service (HTTP mode)..."
    TORDOWN_LISTEN_ADDR="${listen_addr}" \
    TORDOWN_DOWNLOAD_DIR="${download_dir}" \
      "${SERVICE_INSTALLER}"

    echo ""
    echo "Install complete (HTTP mode)."
    echo "Access using: http://<server-ip-or-domain>${listen_addr}"
    ;;
  2)
    domain=""
    email=""

    read -r -p "Domain (example: myhost.duckdns.org): " domain
    read -r -p "Email for Let's Encrypt notices: " email

    if [[ -z "${domain}" || -z "${email}" ]]; then
      echo "Domain and email are required for SSL setup."
      exit 1
    fi

    echo ""
    echo "Installing TorDown service (pre-SSL setup)..."
    TORDOWN_LISTEN_ADDR=":443" \
    TORDOWN_DOWNLOAD_DIR="${download_dir}" \
    TORDOWN_DOMAIN="${domain}" \
      "${SERVICE_INSTALLER}"

    echo ""
    echo "Configuring SSL with Let's Encrypt..."
    chmod +x "${SSL_SETUP_SCRIPT}"
    "${SSL_SETUP_SCRIPT}" "${domain}" "${email}" --challenge standalone

    echo ""
    echo "Install complete (HTTPS mode)."
    echo "Access using: https://${domain}"
    ;;
  *)
    echo "Invalid choice: ${install_mode}"
    exit 1
    ;;
esac

echo ""
echo "Useful commands:"
echo "  sudo systemctl status tordown"
echo "  sudo journalctl -u tordown -f"
echo "  sudo systemctl restart tordown"
