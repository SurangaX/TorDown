# VPS Deployment Instructions - Python Telegram Uploader

## Quick Deployment (Copy-Paste Commands)

Run these commands on your VPS via SSH:

```bash
# 1. Stop current server
pkill -f "bin/tordown" || true
sleep 2

# 2. Navigate to TorDown directory
cd /root/TorDown

# 3. Pull latest code (if using git)
git pull origin main

# 4. Install system dependencies
apt-get update -qq
apt-get install -y -qq python3 python3-pip p7zip-full

# 5. Install Python packages
pip3 install pyrogram==2.0.104 tgcrypto==1.2.5

# 6. Rebuild TorDown with Python integration
GOMAXPROCS=1 go build -p 1 -o bin/tordown ./cmd/server

# 7. Verify build
ls -lh bin/tordown

# 8. Start server
env $(cat .env | xargs) nohup ./bin/tordown > /tmp/tordown.log 2>&1 &

# 9. Check server started
sleep 3
curl -s -k https://tordown.duckdns.org/api/telegram/check

# 10. Check logs
tail -20 /tmp/tordown.log
```

## Expected Output

After deployment, you should see:
```json
{"authenticated":true,"initialized":true}
```

And logs showing:
```
[Telegram] PROGRESS: file.bin: X.X% (XXX/XXX bytes)
[Telegram] FINISHED UPLOAD: file.bin
```

## Verification Checklist

- [ ] Python installed: `python3 --version`
- [ ] 7zip installed: `7z --version`
- [ ] Pyrogram installed: `python3 -c "import pyrogram; print(pyrogram.__version__)"`
- [ ] Binary built: `ls -lh /root/TorDown/bin/tordown`
- [ ] Server running: `ps aux | grep bin/tordown`
- [ ] Server responding: `curl -s -k https://tordown.duckdns.org/api/system`
- [ ] Telegram authenticated: `curl -s -k https://tordown.duckdns.org/api/telegram/check`

## File Locations After Deploy

- Binary: `/root/TorDown/bin/tordown`
- Python uploader: `/root/TorDown/python/telegram_uploader.py`
- Python deps: `/root/TorDown/python/requirements.txt`
- Logs: `/tmp/tordown.log`

## Large File Upload Test

Test with any file >2GB:
```bash
# Monitor upload in real-time
tail -f /tmp/tordown.log | grep -E "UPLOAD|PROGRESS|FINISHED"
```

Expected behavior:
1. File detected as >2GB
2. Split into 2GB parts using 7zip
3. Each part uploaded to Telegram
4. Parts cleaned up after upload
5. Entry marked as uploaded in database

## Troubleshooting

### "python3: command not found"
```bash
apt-get install -y python3 python3-pip
```

### "7z: command not found"
```bash
apt-get install -y p7zip-full
```

### Build fails
```bash
# Check if Go is installed
go version

# Clean and rebuild
cd /root/TorDown
rm -f bin/tordown
GOMAXPROCS=1 go build -p 1 -v -o bin/tordown ./cmd/server
```

### Python uploader not found
```bash
# Verify file exists
ls -l /root/TorDown/python/telegram_uploader.py

# Make it executable
chmod +x /root/TorDown/python/telegram_uploader.py
```

### Server won't start
```bash
# Check full error logs
cat /tmp/tordown.log | tail -50
```

## Rollback (if needed)

If deployment fails, revert to previous version:
```bash
cd /root/TorDown
git revert HEAD
git pull origin main
GOMAXPROCS=1 go build -p 1 -o bin/tordown ./cmd/server
env $(cat .env | xargs) nohup ./bin/tordown > /tmp/tordown.log 2>&1 &
```

## Success Indicators

✓ Server starts without errors
✓ Telegram authenticated: `{"authenticated":true,...}`
✓ API endpoints responding
✓ Small files upload successfully
✓ Large files split and upload in parts
✓ Progress logged to stderr
✓ Files appear in Telegram Saved Messages
