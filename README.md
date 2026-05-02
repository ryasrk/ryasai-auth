# ryasai-auth

Standalone auth agent for ryasai. Three modes: **store** accounts to server queue, **consume** from queue and login, or **local login** directly.

**No Redis, no database, no server setup needed.** Just install, configure, and run.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Machine A (no browser)                                         │
│                                                                 │
│  python ryasai_auth.py accounts.txt --store                     │
│  → POST /api/worker/accounts → server DB + queue                │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │  ryasai server (cloud)  │
                    │                        │
                    │  DB (encrypted) + Queue │
                    └────────────┬───────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│  Machine B (has browser)                                        │
│                                                                 │
│  python ryasai_auth.py --consume                                │
│  → GET /api/worker/jobs (server decrypts passwords)             │
│  → Camoufox login → tokens                                     │
│  → POST /api/worker/results → server token pool                 │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Start

```bash
# 1. Setup
cd ryasai-auth
bash setup.sh

# 2. Configure
nano .env
# Set SERVER_URL and SERVER_ADMIN_KEY

# 3. Create accounts file
cat > accounts.txt << EOF
user1@gmail.com:password123
user2@gmail.com:hunter2
user3@gmail.com:secret456
EOF

# 4. Run (pick one mode)
source .venv/bin/activate

# Option A: Login on this machine
python ryasai_auth.py accounts.txt

# Option B: Push to queue, login elsewhere
python ryasai_auth.py accounts.txt --store

# Option C: Consume queue, login here
python ryasai_auth.py --consume
```

## Usage Modes

### Mode 1: Local Login (default)
Read accounts from file → login on **this machine** → push tokens to server.

```bash
python ryasai_auth.py accounts.txt
```

Best when:
- You have the accounts file AND a browser on this machine
- One-shot: login everything now, push results

### Mode 2: Store (push to queue)
Read accounts from file → push to **server queue** → done.

```bash
python ryasai_auth.py accounts.txt --store
```

Best when:
- This machine has no browser / can't run Camoufox
- You want to add accounts now, login later
- Another machine will `--consume` the queue

### Mode 3: Consume (pull from queue → login → push results)
Pull queued accounts from server → login on **this machine** → push tokens back.

```bash
python ryasai_auth.py --consume
```

Best when:
- Accounts were already pushed via `--store`
- This machine has Camoufox installed
- Runs as a long-lived worker (polls every 10s)

**No accounts file needed** — jobs come from the server queue.

## CLI Options

```
python ryasai_auth.py [accounts_file] [OPTIONS]

Positional:
  accounts_file         Path to accounts file (required for default & --store)

Modes (mutually exclusive):
  --store               Push accounts to server queue (no login here)
  --consume             Pull from server queue, login here, push results

Options:
  --providers PROVIDERS Comma-separated providers (default: from .env)
  --concurrency N       Max concurrent logins (default: from .env)
  --headed              Show browser window (for debugging)
  --proxy URL           Proxy URL for browser sessions
  --poll-interval N     Seconds between queue polls in --consume (default: 10)
```

## Accounts File Format

```
# accounts.txt — one per line
# Lines starting with # are ignored

user1@gmail.com:password123
user2@gmail.com:hunter2
user3@gmail.com:secret456
```

Supported separators: `:`, `\t` (tab), space

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_URL` | `http://localhost:3456` | ryasai server URL |
| `SERVER_ADMIN_KEY` | (required) | Admin key for server API |
| `PROVIDERS` | `kiro,codebuddy` | Default providers to login |
| `CONCURRENCY` | `2` | Max concurrent browser sessions |
| `CAMOUFOX_HEADLESS` | `true` | Browser headless mode |
| `PROXY_URL` | (empty) | Proxy for browser sessions |
| `LOG_LEVEL` | `INFO` | Logging level |

## Available Providers

| Provider | Description |
|----------|-------------|
| `kiro` | Kiro (AWS-based, PKCE OAuth) |
| `codebuddy` | CodeBuddy AI |
| `wavespeed` | Wavespeed |
| `canva` | Canva |
| `yepapi` | YepAPI |

## Examples

```bash
# Login 100 accounts with kiro + codebuddy (default mode)
python ryasai_auth.py accounts.txt

# Login with only kiro
python ryasai_auth.py accounts.txt --providers kiro

# High concurrency (4 browsers at once)
python ryasai_auth.py accounts.txt --concurrency 4

# Debug mode (visible browser)
python ryasai_auth.py accounts.txt --headed --concurrency 1

# With proxy
python ryasai_auth.py accounts.txt --proxy socks5://user:pass@host:port

# Push accounts to queue (no login on this machine)
python ryasai_auth.py accounts.txt --store

# Consume queue on a different machine
python ryasai_auth.py --consume

# Consume with high concurrency and custom providers
python ryasai_auth.py --consume --concurrency 5 --providers kiro,wavespeed

# Consume with proxy
python ryasai_auth.py --consume --proxy socks5://user:pass@host:port

# Consume with faster polling
python ryasai_auth.py --consume --poll-interval 5
```

## Typical Workflow

```bash
# Machine A (office PC, no browser needed):
python ryasai_auth.py accounts.txt --store
# → "✅ Done: added=100, queued=100 (waiting for consumer)"

# Machine B (VPS with Camoufox):
python ryasai_auth.py --consume
# → "📥 Pulled 10 jobs from queue"
# → "✅ user1@gmail.com/kiro — success"
# → "✅ user2@gmail.com/kiro — success"
# → ...keeps polling for more...

# Check queue status on server:
curl -H "Authorization: Bearer $KEY" http://server:3456/api/worker/pending-accounts
curl -H "Authorization: Bearer $KEY" http://server:3456/api/worker/queue-depth
```

## Project Structure

```
ryasai-auth/
├── ryasai_auth.py      # Main CLI entry point
├── config.py           # Settings (from .env)
├── api_client.py       # HTTP client → ryasai server
├── login_runner.py     # Browser login executor
├── browser.py          # Camoufox browser config
├── providers/          # Login adapters per provider
│   ├── base.py         # Abstract base class
│   ├── kiro.py         # Kiro PKCE OAuth login
│   ├── codebuddy.py    # CodeBuddy login
│   ├── wavespeed.py    # Wavespeed login
│   ├── canva.py        # Canva login
│   └── yepapi.py       # YepAPI login
├── errors/             # Error codes & exceptions
│   ├── codes.py
│   └── exceptions.py
├── requirements.txt    # Python dependencies
├── setup.sh            # Quick setup script
├── .env.example        # Environment template
└── README.md           # This file
```
