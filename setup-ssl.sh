#!/usr/bin/env bash
set -euo pipefail

# TorDown SSL setup via Let's Encrypt ACME API (through certbot)
# Usage:
#   sudo ./setup-ssl.sh <domain> <email> [--challenge standalone|webroot] [--webroot-path /var/www/html] [--staging] [--skip-dry-run]

if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root: sudo ./setup-ssl.sh <domain> <email>"
    exit 1
fi

if [[ $# -lt 2 ]]; then
    echo "Usage: sudo ./setup-ssl.sh <domain> <email> [options]"
    echo ""
    echo "Options:"
    echo "  --challenge standalone|webroot   ACME validation mode (default: standalone)"
    echo "  --webroot-path <path>            Required with --challenge webroot"
    echo "  --staging                        Use Let's Encrypt staging endpoint"
    echo "  --skip-dry-run                   Skip certbot renew dry-run test"
    echo ""
    echo "Example:"
    echo "  sudo ./setup-ssl.sh myserver.duckdns.org admin@example.com --challenge standalone"
    exit 1
fi

DOMAIN="$1"
EMAIL="$2"
shift 2

CHALLENGE="standalone"
WEBROOT_PATH=""
USE_STAGING="false"
SKIP_DRY_RUN="false"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --challenge)
            CHALLENGE="${2:-}"
            shift 2
            ;;
        --webroot-path)
            WEBROOT_PATH="${2:-}"
            shift 2
            ;;
        --staging)
            USE_STAGING="true"
            shift
            ;;
        --skip-dry-run)
            SKIP_DRY_RUN="true"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [[ "${CHALLENGE}" != "standalone" && "${CHALLENGE}" != "webroot" ]]; then
    echo "Invalid --challenge value: ${CHALLENGE}"
    exit 1
fi

if [[ "${CHALLENGE}" == "webroot" && -z "${WEBROOT_PATH}" ]]; then
    echo "--webroot-path is required with --challenge webroot"
    exit 1
fi

if [[ "${CHALLENGE}" == "webroot" && ! -d "${WEBROOT_PATH}" ]]; then
    echo "Webroot path does not exist: ${WEBROOT_PATH}"
    exit 1
fi

SERVICE_NAME="tordown"
ENV_FILE="/etc/tordown.env"
LE_LIVE_DIR="/etc/letsencrypt/live/${DOMAIN}"
CERT_PATH="${LE_LIVE_DIR}/fullchain.pem"
KEY_PATH="${LE_LIVE_DIR}/privkey.pem"
HOOK_PATH="/usr/local/bin/tordown-cert-renew-hook.sh"

echo "=== Installing certbot ==="
apt-get update
apt-get install -y certbot

STOPPED_SERVICE="false"
if systemctl list-unit-files | grep -q "^${SERVICE_NAME}\.service"; then
    if systemctl is-active --quiet "${SERVICE_NAME}" && [[ "${CHALLENGE}" == "standalone" ]]; then
        echo "=== Stopping ${SERVICE_NAME} temporarily for standalone challenge ==="
        systemctl stop "${SERVICE_NAME}"
        STOPPED_SERVICE="true"
    fi
fi

CERTBOT_ARGS=(
    certonly
    --agree-tos
    --email "${EMAIL}"
    --non-interactive
    --keep-until-expiring
    -d "${DOMAIN}"
)

if [[ "${USE_STAGING}" == "true" ]]; then
    CERTBOT_ARGS+=(--staging)
fi

if [[ "${CHALLENGE}" == "standalone" ]]; then
    CERTBOT_ARGS+=(--standalone --preferred-challenges http)
else
    CERTBOT_ARGS+=(--webroot -w "${WEBROOT_PATH}")
fi

echo "=== Requesting certificate for ${DOMAIN} ==="
certbot "${CERTBOT_ARGS[@]}"

if [[ ! -f "${CERT_PATH}" || ! -f "${KEY_PATH}" ]]; then
    echo "Certificate files not found after issuance."
    exit 1
fi

echo "=== Configuring TorDown environment ==="
if [[ ! -f "${ENV_FILE}" ]]; then
    cat > "${ENV_FILE}" <<EOF
TORDOWN_LISTEN_ADDR=:8080
TORDOWN_DOWNLOAD_DIR=/var/lib/tordown/downloads
EOF
fi

if grep -q '^TORDOWN_SSL_CERT=' "${ENV_FILE}"; then
    sed -i "s|^TORDOWN_SSL_CERT=.*|TORDOWN_SSL_CERT=${CERT_PATH}|" "${ENV_FILE}"
else
    echo "TORDOWN_SSL_CERT=${CERT_PATH}" >> "${ENV_FILE}"
fi

if grep -q '^TORDOWN_SSL_KEY=' "${ENV_FILE}"; then
    sed -i "s|^TORDOWN_SSL_KEY=.*|TORDOWN_SSL_KEY=${KEY_PATH}|" "${ENV_FILE}"
else
    echo "TORDOWN_SSL_KEY=${KEY_PATH}" >> "${ENV_FILE}"
fi

echo "=== Installing renew deploy hook ==="
cat > "${HOOK_PATH}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="tordown"
if systemctl list-unit-files | grep -q "^${SERVICE_NAME}\.service"; then
    systemctl restart "${SERVICE_NAME}"
fi
EOF
chmod 0755 "${HOOK_PATH}"

echo "=== Enabling automatic renewal ==="
mkdir -p /etc/letsencrypt/renewal-hooks/deploy
ln -sf "${HOOK_PATH}" /etc/letsencrypt/renewal-hooks/deploy/tordown-restart.sh

# Prefer systemd timer on Ubuntu. Keep cron fallback if timer is unavailable.
if systemctl list-unit-files | grep -q '^certbot.timer'; then
    systemctl enable --now certbot.timer
else
    CRON_LINE="0 3,15 * * * certbot renew -q"
    (crontab -l 2>/dev/null | grep -v 'certbot renew -q'; echo "${CRON_LINE}") | crontab -
fi

if [[ "${SKIP_DRY_RUN}" != "true" ]]; then
    echo "=== Testing renewal (dry-run) ==="
    certbot renew --dry-run
fi

if systemctl list-unit-files | grep -q "^${SERVICE_NAME}\.service"; then
    echo "=== Restarting ${SERVICE_NAME} with TLS ==="
    systemctl restart "${SERVICE_NAME}"
    systemctl --no-pager --full status "${SERVICE_NAME}" | head -n 12 || true
elif [[ "${STOPPED_SERVICE}" == "true" ]]; then
    echo "=== Restarting ${SERVICE_NAME} ==="
    systemctl start "${SERVICE_NAME}"
fi

echo ""
echo "SSL setup complete."
echo "Domain: ${DOMAIN}"
echo "Certificate: ${CERT_PATH}"
echo "Key: ${KEY_PATH}"
echo ""
echo "TorDown should now be reachable at: https://${DOMAIN}:8080"
echo "If it is not reachable, verify inbound TCP 80 and 8080 on your firewall/router."
