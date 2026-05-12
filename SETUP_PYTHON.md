# TorDown Large File Upload Setup

## Prerequisites

### 1. Python 3.9+
```bash
# Linux
sudo apt-get install python3 python3-pip

# Windows
# Download from https://www.python.org/downloads/
```

### 2. Python Dependencies
```bash
cd python
pip install -r requirements.txt
```

### 3. 7zip (for file splitting)
```bash
# Linux
sudo apt-get install p7zip-full

# macOS
brew install p7zip

# Windows
choco install 7zip
# OR download from https://www.7-zip.org/
```

## Building TorDown

```bash
# Build Go binary (Python uploader will be called at runtime)
GOMAXPROCS=1 go build -p 1 -o bin/tordown ./cmd/server
```

## Deployment

1. Copy the `python/` directory alongside the binary
2. Ensure both Python and 7zip are installed on deployment system
3. Start the server normally

## File Upload Flow

### Small Files (<2GB)
- Direct upload via pyrogram to Telegram Saved Messages
- Single transaction, no splitting

### Large Files (>2GB)
1. File split into 2GB parts using 7zip
2. Each part uploaded separately to Telegram
3. Parts cleaned up after successful upload
4. Pyrogram handles authentication and multipart uploads

## Troubleshooting

### "python3 not found"
- Ensure Python 3 is installed and in PATH
- Windows: Use `python` instead of `python3`

### "7z not found" 
- Install 7zip (see Prerequisites)
- On Linux, ensure path includes `/usr/bin/7z`

### Upload failures
- Check Python logs in stderr
- Verify Telegram authentication is active
- Ensure sufficient disk space for split files (>2GB for large files)
