#!/bin/bash
# Deploy TorDown with Python Telegram uploader to VPS

set -e

TORDOWN_DIR="/root/TorDown"
PYTHON_DIR="$TORDOWN_DIR/python"

echo "=== TorDown Deployment with Python Uploader ==="

# 1. Install system dependencies
echo "[1/5] Installing system dependencies..."
apt-get update -qq
apt-get install -y -qq python3 python3-pip p7zip-full

# 2. Create Python environment
echo "[2/5] Setting up Python environment..."
mkdir -p "$PYTHON_DIR"
cd "$TORDOWN_DIR"

# Install Python requirements
pip3 install -q -r "$PYTHON_DIR/requirements.txt"

# 3. Build Go binary
echo "[3/5] Building TorDown binary..."
cd "$TORDOWN_DIR"
GOMAXPROCS=1 go build -p 1 -v -o bin/tordown ./cmd/server

# 4. Verify setup
echo "[4/5] Verifying installation..."
python3 --version
7z --version | head -1
which python3
which 7z

# 5. Test Python uploader
echo "[5/5] Testing Python uploader script..."
if [ -f "$PYTHON_DIR/telegram_uploader.py" ]; then
    chmod +x "$PYTHON_DIR/telegram_uploader.py"
    echo "✓ Python uploader ready"
else
    echo "✗ Python uploader not found!"
    exit 1
fi

echo ""
echo "=== Deployment Complete ==="
echo "To start the server:"
echo "  cd $TORDOWN_DIR"
echo "  env \$(cat .env | xargs) nohup ./bin/tordown > /tmp/tordown.log 2>&1 &"
echo ""
echo "Python uploader will be automatically used for file uploads >2GB"
