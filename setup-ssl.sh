#!/bin/bash

# SSL Setup Script for TorDown with DuckDNS and Let's Encrypt
# Usage: sudo ./setup-ssl.sh your-subdomain.duckdns.org your-email@example.com

set -e

if [ "$EUID" -ne 0 ]; then 
    echo "Please run as root (use sudo)"
    exit 1
fi

if [ -z "$1" ] || [ -z "$2" ]; then
    echo "Usage: sudo ./setup-ssl.sh <your-domain.duckdns.org> <your-email>"
    echo "Example: sudo ./setup-ssl.sh myserver.duckdns.org admin@example.com"
    exit 1
fi

DOMAIN=$1
EMAIL=$2

echo "=== Installing Certbot ==="
apt-get update
apt-get install -y certbot

echo ""
echo "=== Stopping TorDown if running ==="
pkill tordown || true
sleep 2

echo ""
echo "=== Obtaining SSL Certificate ==="
echo "This will use Let's Encrypt to get a free SSL certificate"
echo "Make sure port 80 is open and accessible from the internet!"
echo ""

certbot certonly --standalone \
    --preferred-challenges http \
    --agree-tos \
    --email "$EMAIL" \
    --non-interactive \
    -d "$DOMAIN"

if [ $? -eq 0 ]; then
    echo ""
    echo "=== SSL Certificate obtained successfully! ==="
    echo ""
    echo "Certificate location: /etc/letsencrypt/live/$DOMAIN/fullchain.pem"
    echo "Private key location: /etc/letsencrypt/live/$DOMAIN/privkey.pem"
    echo ""
    echo "=== Setting up auto-renewal ==="
    
    # Test renewal
    certbot renew --dry-run
    
    # Setup cron job for auto-renewal
    CRON_JOB="0 0,12 * * * certbot renew --quiet --deploy-hook 'pkill -HUP tordown'"
    (crontab -l 2>/dev/null | grep -v certbot; echo "$CRON_JOB") | crontab -
    
    echo ""
    echo "=== Creating systemd service ==="
    
    cat > /etc/systemd/system/tordown.service <<EOF
[Unit]
Description=TorDown Torrent Web Manager
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$(pwd)
Environment="TORDOWN_LISTEN_ADDR=:8080"
Environment="TORDOWN_DOWNLOAD_DIR=./downloads"
Environment="TORDOWN_SSL_CERT=/etc/letsencrypt/live/$DOMAIN/fullchain.pem"
Environment="TORDOWN_SSL_KEY=/etc/letsencrypt/live/$DOMAIN/privkey.pem"
ExecStart=$(pwd)/tordown
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable tordown
    
    echo ""
    echo "=== SSL Setup Complete! ==="
    echo ""
    echo "Next steps:"
    echo "1. Update your application to use HTTPS (see instructions below)"
    echo "2. Start the service: sudo systemctl start tordown"
    echo "3. Check status: sudo systemctl status tordown"
    echo "4. View logs: sudo journalctl -u tordown -f"
    echo ""
    echo "Your site will be available at: https://$DOMAIN:8080"
    echo ""
    echo "Certificate will auto-renew every 60 days via cron job"
    echo ""
else
    echo ""
    echo "=== SSL Certificate failed ==="
    echo "Please check:"
    echo "1. Port 80 is open and accessible"
    echo "2. Domain $DOMAIN points to this server's IP"
    echo "3. Check logs above for errors"
    exit 1
fi
