# TorDown

TorDown is a minimal web UI for managing torrents backed by [`github.com/anacrolix/torrent`](https://github.com/anacrolix/torrent). It targets headless Ubuntu servers but runs anywhere Go is available. The server exposes a small JSON API and serves a lightweight single-page application for quick remote management.

## Features

- Add torrents via magnet URI, direct `.torrent` URL fetch, or uploaded `.torrent` file
- **File selection popup on add** - Choose which files to download immediately after adding a torrent
- Pause, resume, remove, or force recheck torrents (with optional on-disk data removal)
- Live progress, transfer rates, ETA, and peer counts per torrent
- Per-file selection so you can download only the pieces you need
- **Download completed files from server to your PC** with one-click per-file downloads
- **Download entire torrent as ZIP** - packages all completed files into a single archive
- **Built-in video player** - stream and watch movies directly in the browser (supports MP4, WebM, MKV, and more)
- **Server resource monitor** - circular gauges for real-time CPU, RAM, and storage monitoring with live updates
- **Accurate download size reporting** - storage tile shows true download folder usage
- **Clear Data action** - remove orphaned leftover files and purge ZIP cache files from `/tmp/tordown-zip-cache`
- **Incomplete file delete action** - remove partial files directly from torrent details
- **Resumable downloads** - file and ZIP downloads support HTTP range requests for browser pause/resume
- **Large ZIP stability** - big archives are prepared in background before download starts
- **Prepared ZIP workflow** - UI prepares ZIP in background, then starts a range-capable download
- **ZIP prepare progress** - live percent, processed size, and ETA shown while archive is being built
- **Dark mode / Light mode** - automatic theme support with persistent user preference
- **HTTPS/SSL with Let's Encrypt** - automatic HTTP → HTTPS redirect, production-ready certificate management
- **Modern UI icons** - clean SVG icons for theme toggle, refresh, and ZIP download actions
- Session overview panel with live stats plus an interactive details drawer
- Static HTML/JS frontend with automatic polling (no external dependencies)
- Minimalistic, clean UI design for efficient torrent management
- Fully responsive mobile view

## Requirements

- Go 1.22+
- Network access for torrent peers and trackers

## Getting Started

```powershell
# Install dependencies (first run only)
go get github.com/shirou/gopsutil/v3
go mod tidy

# Run the HTTP server
go run ./cmd/server
```

By default the server listens on `:8080` (HTTP) and stores data in `./downloads`. Visit `http://localhost:8080` to access the UI. For production with SSL, the server listens on `:443` (HTTPS) with automatic HTTP redirect on port 80.

## Ubuntu Server Startup

### SSL/HTTPS Setup (Recommended)

For production deployments, set up HTTPS with Let's Encrypt:

```bash
chmod +x setup-ssl.sh
sudo ./setup-ssl.sh your-domain.duckdns.org your-email@example.com
```

This script will:
- Install certbot and obtain an SSL certificate
- Configure TorDown to listen on port 443 (HTTPS)
- Set up automatic HTTP → HTTPS redirect on port 80
- Enable automatic certificate renewal

See [SSL-SETUP.md](SSL-SETUP.md) for detailed SSL configuration, including webroot challenge mode and staging endpoint testing.

### Quick Start (Foreground)

Use this when you want to run TorDown directly in your shell:

```bash
chmod +x scripts/start-ubuntu.sh
./scripts/start-ubuntu.sh
```

Optional environment overrides:

```bash
TORDOWN_LISTEN_ADDR=:8080 TORDOWN_DOWNLOAD_DIR=/srv/tordown/downloads ./scripts/start-ubuntu.sh
```

### Production Start (systemd, Auto-Start On Boot)

Use this on an Ubuntu server for persistent background operation:

```bash
chmod +x scripts/install-ubuntu-service.sh
sudo ./scripts/install-ubuntu-service.sh
```

Useful service commands:

```bash
sudo systemctl status tordown
sudo journalctl -u tordown -f
sudo systemctl restart tordown
```

Service environment is stored in `/etc/tordown.env`.
After changing it, reload with:

```bash
sudo systemctl restart tordown
```

### Configuration

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `TORDOWN_LISTEN_ADDR` | `:443` (with SSL) or `:8080` | Address and port for the server |
| `TORDOWN_DOWNLOAD_DIR` | `./downloads` | Directory where torrent data is stored |
| `TORDOWN_DOMAIN` | (none) | Domain name for HTTP → HTTPS redirect (e.g., `example.duckdns.org`) |
| `TORDOWN_SSL_CERT` | (none) | Path to SSL certificate file (enables HTTPS) |
| `TORDOWN_SSL_KEY` | (none) | Path to SSL private key file (enables HTTPS) |

**HTTP vs HTTPS:**
- **Without SSL variables:** Runs plain HTTP on configured port (default `:8080`)
- **With SSL variables:** Runs HTTPS on configured port (default `:443`) + HTTP redirect server on port 80

### API Overview

All endpoints live under `/api` and respond with JSON.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | Health check |
| `GET` | `/api/stats` | Aggregate client stats |
| `GET` | `/api/system` | Server resource monitoring (CPU, RAM, storage, network) |
| `POST` | `/api/data/cleanup` | Remove orphaned data not tied to active torrents |
| `GET` | `/api/torrents` | List torrents |
| `POST` | `/api/torrents` | Add by magnet URI (`magnetUri`), torrent URL (`torrentUrl`), or file (`torrentFile` base64) |
| `GET` | `/api/torrents/{infoHash}` | Detailed torrent info (returns `202 Accepted` while metadata is pending) |
| `DELETE` | `/api/torrents/{infoHash}` | Remove torrent (`?deleteData=true` also prunes files) |
| `POST` | `/api/torrents/{infoHash}/pause` | Pause transfers |
| `POST` | `/api/torrents/{infoHash}/resume` | Resume transfers |
| `POST` | `/api/torrents/{infoHash}/verify` | Trigger recheck |
| `POST` | `/api/torrents/{infoHash}/selection` | Update file selection for the torrent |
| `GET` | `/api/torrents/{infoHash}/files/{fileIndex}` | Download a specific file to your PC |
| `DELETE` | `/api/torrents/{infoHash}/files/{fileIndex}` | Delete a specific incomplete file from disk |
| `GET` | `/api/torrents/{infoHash}/download-zip` | Download all completed files as a ZIP archive |
| `GET` | `/api/torrents/{infoHash}/files/{fileIndex}` | **Download completed file to your PC** |

For the selection endpoint, send `{"applySelection": true, "selectedFiles": [0,2,5]}` to download only specific file indices, or `{"applySelection": false}` to revert to downloading everything.

## Development Notes

- The Go server uses `chi` for routing and wraps the torrent client behind `internal/torrent`.
- Static assets live in `web/` and are served directly by the Go process.
- Long-running file and ZIP download endpoints bypass API request timeout to avoid interrupted large transfers.
- When Go tooling is unavailable, run `go mod tidy` on a machine with Go installed to generate `go.sum` and download dependencies.
- Frontend uses vanilla JavaScript with CSS custom properties for theme support (dark mode on by default).
- Circular resource gauges use CSS `conic-gradient` for live CPU, RAM, and disk monitoring.
- SSL/HTTPS mode spawns two listeners: main HTTPS server on port 443 and HTTP redirect server on port 80 (both share graceful shutdown).
- Certificate renewal via certbot automatically restarts the tordown service via deploy hook.

## License

This project is distributed under the MIT License. See `LICENSE` (if present) for details.
