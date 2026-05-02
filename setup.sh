#!/usr/bin/env bash
# ╔══════════════════════════════════════════════════════════════════╗
# ║  ryasai-auth — Setup                                            ║
# ║  Creates venv, installs deps, downloads Camoufox browser        ║
# ║                                                                  ║
# ║  Usage:  chmod +x setup.sh && ./setup.sh                         ║
# ╚══════════════════════════════════════════════════════════════════╝

set -euo pipefail

# ── Colors ───────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC}  $1"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $1"; }
err()   { echo -e "${RED}[ERR]${NC}   $1"; }
step()  { echo -e "\n${CYAN}━━━ $1 ━━━${NC}"; }

# ── Config ───────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/.venv"

cd "$SCRIPT_DIR"

echo -e "${CYAN}"
echo "  ⚡ ryasai-auth — Setup"
echo "  ──────────────────────"
echo -e "${NC}"

# ── Step 1: Python ───────────────────────────────────────────────
step "1/5 — Python Environment"

PYTHON=""
for cmd in python3 python3.12 python3.11 python3.10; do
    if command -v "$cmd" &>/dev/null; then
        ver=$("$cmd" --version 2>&1 | grep -oP '\d+\.\d+')
        major=$(echo "$ver" | cut -d. -f1)
        minor=$(echo "$ver" | cut -d. -f2)
        if [[ "$major" -ge 3 && "$minor" -ge 10 ]]; then
            PYTHON="$cmd"
            break
        fi
    fi
done

if [[ -z "$PYTHON" ]]; then
    err "Python 3.10+ required. Install: sudo apt install python3.12 python3.12-venv"
    exit 1
fi
ok "Python: $($PYTHON --version) ($(command -v $PYTHON))"

# ── Step 2: Virtual Environment ──────────────────────────────────
step "2/5 — Virtual Environment"

if [[ -d "$VENV_DIR" && -f "$VENV_DIR/bin/python" ]]; then
    ok "Venv exists: $VENV_DIR"
else
    [[ -d "$VENV_DIR" ]] && rm -rf "$VENV_DIR"

    info "Creating venv..."
    if ! "$PYTHON" -m venv "$VENV_DIR" 2>/dev/null; then
        err "Failed to create venv. Installing python3-venv..."
        if [[ $EUID -eq 0 ]]; then
            apt-get install -y -qq python3-venv "$(basename $PYTHON)-venv" 2>/dev/null || true
        else
            sudo apt-get install -y -qq python3-venv "$(basename $PYTHON)-venv" 2>/dev/null || true
        fi
        "$PYTHON" -m venv "$VENV_DIR" || {
            err "Cannot create venv. Install: sudo apt install $(basename $PYTHON)-venv"
            exit 1
        }
    fi
    ok "Created: $VENV_DIR"
fi

source "$VENV_DIR/bin/activate"
pip install --upgrade pip -q

# ── Step 3: Python Dependencies ──────────────────────────────────
step "3/5 — Python Dependencies"

info "Installing requirements..."
pip install -r "$SCRIPT_DIR/requirements.txt" -q
ok "All Python packages installed"

# ── Step 4: Camoufox Browser ─────────────────────────────────────
step "4/5 — Camoufox Browser + Playwright"

# System deps for headless browser (optional but recommended)
SYSTEM_DEPS="libatk-bridge2.0-0 libatk1.0-0 libcups2 libglib2.0-0 libgtk-3-0 libnspr4 libnss3 libxcomposite1 libxdamage1 libxrandr2 xvfb"
if command -v apt-get &>/dev/null; then
    info "Installing browser system dependencies..."
    if [[ $EUID -eq 0 ]]; then
        apt-get install -y -qq $SYSTEM_DEPS 2>/dev/null && ok "System deps installed" || warn "Some system deps failed (browser may still work)"
    else
        sudo apt-get install -y -qq $SYSTEM_DEPS 2>/dev/null && ok "System deps installed" || warn "Some system deps failed (browser may still work)"
    fi
else
    warn "Not Debian/Ubuntu — skipping system deps. Install browser libs manually if needed."
fi

# Download Camoufox Firefox binary (~200MB)
info "Downloading Camoufox browser..."
python -m camoufox fetch && ok "Camoufox browser downloaded" || {
    err "Failed to download Camoufox. Run manually: python -m camoufox fetch"
    exit 1
}

# Install Playwright Firefox (fallback / dependency)
info "Installing Playwright Firefox..."
python -m playwright install firefox && ok "Playwright Firefox installed" || {
    warn "Playwright Firefox install failed (Camoufox should still work)"
}

# ── Step 5: Environment Config ───────────────────────────────────
step "5/5 — Environment Config"

if [[ ! -f "$SCRIPT_DIR/.env" ]]; then
    cp "$SCRIPT_DIR/.env.example" "$SCRIPT_DIR/.env"
    ok "Created .env from template"
    warn "⚠️  Edit .env — set SERVER_URL and SERVER_ADMIN_KEY"
else
    ok ".env already exists"
fi

# ── Done ─────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  ✅ ryasai-auth setup complete!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${CYAN}Next steps:${NC}"
echo ""
echo -e "    ${YELLOW}1. Configure:${NC}"
echo -e "       nano .env"
echo -e "       # Set SERVER_URL=https://your-server.com"
echo -e "       # Set SERVER_ADMIN_KEY=your-admin-key"
echo ""
echo -e "    ${YELLOW}2. Create accounts file:${NC}"
echo -e "       echo 'user@gmail.com:password123' > accounts.txt"
echo ""
echo -e "    ${YELLOW}3. Run (pick one):${NC}"
echo ""
echo -e "       # Login on this machine, push tokens to server"
echo -e "       source .venv/bin/activate"
echo -e "       python ryasai_auth.py accounts.txt"
echo ""
echo -e "       # Push accounts to server queue (no login here)"
echo -e "       python ryasai_auth.py accounts.txt --store"
echo ""
echo -e "       # Consume queue from server, login here"
echo -e "       python ryasai_auth.py --consume"
echo ""
