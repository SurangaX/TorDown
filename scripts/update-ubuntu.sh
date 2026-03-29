#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== TorDown Server Update ===${NC}"

# Verify we're in the right directory
if [[ ! -d ".git" ]] || [[ ! -f "go.mod" ]]; then
  echo -e "${RED}Error: Not in TorDown directory${NC}"
  exit 1
fi

# Step 1: Fetch latest changes
echo -e "\n${YELLOW}1. Pulling latest changes from git...${NC}"
git pull origin main || { echo -e "${RED}Failed to pull from git${NC}"; exit 1; }
echo -e "${GREEN}✓ Git pull successful${NC}"

# Step 2: Check if Go is available in expected path
echo -e "\n${YELLOW}2. Checking Go installation...${NC}"
if [[ -f "/usr/local/go/bin/go" ]]; then
  export PATH="/usr/local/go/bin:$PATH"
  echo -e "${GREEN}✓ Go found at /usr/local/go/bin${NC}"
elif command -v go >/dev/null 2>&1; then
  echo -e "${GREEN}✓ Go found in PATH${NC}"
else
  echo -e "${RED}Error: Go not found. Install Go 1.22+ first${NC}"
  exit 1
fi

# Step 3: Run installation/build script
echo -e "\n${YELLOW}3. Building and installing service...${NC}"
if [[ ! -x "./scripts/install-ubuntu-service.sh" ]]; then
  chmod +x ./scripts/install-ubuntu-service.sh
fi

sudo env PATH="$PATH" ./scripts/install-ubuntu-service.sh || { echo -e "${RED}Failed to install service${NC}"; exit 1; }
echo -e "${GREEN}✓ Service installation successful${NC}"

# Step 4: Restart the service
echo -e "\n${YELLOW}4. Restarting TorDown service...${NC}"
sudo systemctl restart tordown || { echo -e "${RED}Failed to restart service${NC}"; exit 1; }
echo -e "${GREEN}✓ Service restarted${NC}"

# Step 5: Verify service is running
echo -e "\n${YELLOW}5. Verifying service status...${NC}"
if sudo systemctl is-active --quiet tordown; then
  echo -e "${GREEN}✓ TorDown service is running${NC}"
  
  # Show service status
  echo -e "\n${YELLOW}Service Status:${NC}"
  sudo systemctl status tordown --no-pager | head -n 5
  
  echo -e "\n${GREEN}=== Update Complete ===${NC}"
  exit 0
else
  echo -e "${RED}✗ Service failed to start${NC}"
  echo -e "\n${YELLOW}Service logs:${NC}"
  sudo journalctl -u tordown -n 20 --no-pager
  exit 1
fi
