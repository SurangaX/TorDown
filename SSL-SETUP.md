# SSL Setup Guide for TorDown with DuckDNS

This guide will help you set up HTTPS/SSL for your TorDown server using Let's Encrypt and DuckDNS.

## Prerequisites

1. A DuckDNS account and subdomain (e.g., `myserver.duckdns.org`)
2. Your server's public IP configured in DuckDNS
3. Port 80 and 443 open on your firewall/VPS control panel
4. Root access to your Ubuntu server

## Quick Setup

1. **Make the setup script executable:**
   ```bash
   chmod +x setup-ssl.sh
   ```

2. **Run the SSL setup script (standalone challenge):**
   ```bash
   sudo ./setup-ssl.sh your-subdomain.duckdns.org your-email@example.com
   ```
   
   Example:
   ```bash
   sudo ./setup-ssl.sh myserver.duckdns.org admin@gmail.com
   ```

   Optional flags:
   ```bash
   # Use Let's Encrypt staging (test mode)
   sudo ./setup-ssl.sh myserver.duckdns.org admin@gmail.com --staging

   # Use webroot challenge if another web server owns port 80
   sudo ./setup-ssl.sh myserver.duckdns.org admin@gmail.com --challenge webroot --webroot-path /var/www/html
   ```

3. **Rebuild TorDown with SSL support:**
   ```bash
   go build -o tordown ./cmd/server
   ```

4. **Start the service:**
   ```bash
   sudo systemctl start tordown
   ```

5. **Check if it's running:**
   ```bash
   sudo systemctl status tordown
   ```

6. **Access your site:**
   ```
   https://your-subdomain.duckdns.org
   ```
   
   HTTP traffic automatically redirects to HTTPS:
   ```
   http://your-subdomain.duckdns.org → https://your-subdomain.duckdns.org
   ```

## Manual Setup (Alternative)

If you prefer to set up SSL manually:

### 1. Install Certbot
```bash
sudo apt-get update
sudo apt-get install -y certbot
```

### 2. Stop TorDown
```bash
pkill tordown
```

### 3. Obtain SSL Certificate (ACME via certbot)
```bash
sudo certbot certonly --standalone \
  --preferred-challenges http \
  --agree-tos \
  --email your-email@example.com \
  -d your-subdomain.duckdns.org
```

### 4. Set Environment Variables
```bash
export TORDOWN_SSL_CERT=/etc/letsencrypt/live/your-subdomain.duckdns.org/fullchain.pem
export TORDOWN_SSL_KEY=/etc/letsencrypt/live/your-subdomain.duckdns.org/privkey.pem
export TORDOWN_LISTEN_ADDR=:443
export TORDOWN_DOMAIN=your-subdomain.duckdns.org
```

### 5. Build and Run
```bash
go build -o tordown ./cmd/server
sudo -E ./tordown
```

## Using Systemd Service

The setup script creates a systemd service. Here are useful commands:

```bash
# Start the service
sudo systemctl start tordown

# Stop the service
sudo systemctl stop tordown

# Restart the service
sudo systemctl restart tordown

# Check status
sudo systemctl status tordown

# View logs
sudo journalctl -u tordown -f

# Enable auto-start on boot
sudo systemctl enable tordown

# Disable auto-start
sudo systemctl disable tordown
```

## Certificate Renewal

Let's Encrypt certificates expire after 90 days. The setup script configures automatic renewal via `certbot.timer` (or cron fallback) and installs a deploy hook that restarts `tordown` after successful renewal.

### Manual Renewal Test
```bash
sudo certbot renew --dry-run
```

### Force Renewal
```bash
sudo certbot renew
sudo systemctl restart tordown
```

## Troubleshooting

### Port 80 Already in Use
If you have another web server (nginx, apache) running:
```bash
sudo systemctl stop nginx
# or
sudo systemctl stop apache2
```

### Certificate Validation Failed
Make sure:
- Your DuckDNS domain points to your server's public IP
- Port 80 is accessible from the internet
- Your router/firewall allows incoming traffic on port 80

Check your public IP:
```bash
curl ifconfig.me
```

### Permission Denied
SSL certificates require root access:
```bash
sudo ./tordown
# or use systemd
sudo systemctl start tordown
```

## HTTP → HTTPS Redirect

The setup script automatically configures HTTP→HTTPS redirect:
- **Port 80 (HTTP):** Redirects all traffic to HTTPS on port 443
- **Port 443 (HTTPS):** Serves TorDown with SSL certificate

No additional configuration needed. All HTTP requests are redirected with a 301 (permanent) status.

### Example Redirects:
```
http://your-subdomain.duckdns.org       → https://your-subdomain.duckdns.org
http://your-subdomain.duckdns.org/api   → https://your-subdomain.duckdns.org/api
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TORDOWN_LISTEN_ADDR` | Server address and port (HTTPS when certs present) | `:443` |
| `TORDOWN_DOWNLOAD_DIR` | Directory for downloads | `./downloads` |
| `TORDOWN_DOMAIN` | Domain name for HTTP→HTTPS redirect | (defaults to request host) |
| `TORDOWN_SSL_CERT` | Path to SSL certificate file | (none - uses HTTP only) |
| `TORDOWN_SSL_KEY` | Path to SSL private key file | (none - uses HTTP only) |

## Security Notes

- Keep your private key (`privkey.pem`) secure
- Never commit SSL certificates to git
- Use strong passwords for your server
- Consider using a firewall (ufw)
- Regularly update your system and dependencies

## Port Configuration

**For SSL setup and operation:**

| Port | Protocol | Purpose |
|------|----------|----------|
| 80 | TCP | Let's Encrypt ACME validation + HTTP→HTTPS redirect |
| 443 | TCP | TorDown HTTPS server (default with SSL) |
| 42069 | TCP/UDP | Torrent peer connections |

**If behind a router:** Forward ports 80 and 443 to your VPS or forward your VPS ports directly at the control panel.

## Need Help?

- Let's Encrypt: https://letsencrypt.org/docs/
- DuckDNS: https://www.duckdns.org/
- Certbot: https://certbot.eff.org/
