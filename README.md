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
- **Server resource monitor** - real-time CPU, RAM, storage, and network speed monitoring
- **Clear Data action** - remove orphaned leftover files from the download directory
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

By default the server listens on `:8080` and stores data in `./downloads`. Visit `http://localhost:8080` to access the UI.

## Ubuntu Server Startup

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
| `TORDOWN_LISTEN_ADDR` | `:8080` | Address for the HTTP listener |
| `TORDOWN_DOWNLOAD_DIR` | `./downloads` | Directory where torrent data is stored |

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
| `GET` | `/api/torrents/{infoHash}/download-zip` | Download all completed files as a ZIP archive |
| `GET` | `/api/torrents/{infoHash}/files/{fileIndex}` | **Download completed file to your PC** |

For the selection endpoint, send `{"applySelection": true, "selectedFiles": [0,2,5]}` to download only specific file indices, or `{"applySelection": false}` to revert to downloading everything.

## Development Notes

- The Go server uses `chi` for routing and wraps the torrent client behind `internal/torrent`.
- Static assets live in `web/` and are served directly by the Go process.
- When Go tooling is unavailable, run `go mod tidy` on a machine with Go installed to generate `go.sum` and download dependencies.

## License

This project is distributed under the MIT License. See `LICENSE` (if present) for details.
