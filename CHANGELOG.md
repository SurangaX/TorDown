# Changelog

All notable changes to TorDown are documented in this file.

## V1

### Added
- **Production-grade HTTPS/SSL Support** - Automatic HTTP→HTTPS redirect, Let's Encrypt ACME integration, and certificate renewal
- **Dark Mode / Light Mode** - Theme toggle with persistent localStorage preference (dark mode by default)
- **Circular Resource Gauges** - Real-time CPU, RAM, and disk monitoring with animated CSS conic-gradient visualization
- **Modern SVG Icons** - Replaced emoji icons with crisp stroke-based SVG for theme toggle, refresh, and ZIP actions
- **HTTP Redirect Server** - Automatic port 80→443 redirect when SSL is enabled
- **Environment Variables** - Added `TORDOWN_DOMAIN`, `TORDOWN_LISTEN_ADDR`, `TORDOWN_SSL_CERT`, `TORDOWN_SSL_KEY` for flexible deployment

### Changed
- **Default Listen Port** - Changed from `:8080` to `:443` when SSL certificates are configured
- **Server Architecture** - Dual-listener design: main HTTPS server + HTTP redirect server with shared graceful shutdown
- **Setup Script** - Enhanced `setup-ssl.sh` with Let's Encrypt ACME automation, challenge mode selection, and auto-renewal configuration
- **Documentation** - Updated README.md with SSL setup instructions, dark mode highlights, and environment variable documentation

### Fixed
- **Dark Mode CSS** - Corrected CSS variable scoping and removed malformed nested media-query rules
- **Storage Gauge Layout** - Added `flex-shrink: 0` to prevent circular gauge compression from long text nodes
- **Upload Button Visibility** - File input now has visible label trigger for `.torrent` uploads
- **Table Column Alignment** - Added missing Status column header and verified all 8 columns render correctly
- **Compiler Error** - Removed unused variable in `net.SplitHostPort` call

### Tech Stack
- Go 1.22+ backend with chi HTTP router
- vanilla JavaScript frontend with CSS3 variables and conic-gradient gauges
- Let's Encrypt + certbot for SSL certificate management
- anacrolix/torrent for BitTorrent protocol implementation

### Deployment Notes
- Requires root for ports 80 and 443 (standard HTTPS practice)
- Ubuntu 18.04+ recommended for certbot.timer auto-renewal
- Port 80 must be open for Let's Encrypt ACME validation and HTTP→HTTPS redirect
- Backward compatible: runs on HTTP if SSL environment variables are not set

---

## Previous Releases

See [GitHub Releases](https://github.com/SurangaX/TorDown/releases) for older versions.
