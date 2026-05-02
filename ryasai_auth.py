#!/usr/bin/env python3
"""ryasai-auth — Standalone auth agent for ryasai.

Three modes:
  1. LOCAL LOGIN (default):  Read accounts.txt → login on this PC → push tokens to server
  2. STORE ONLY (--store):   Read accounts.txt → push to server queue → done
  3. CONSUME (--consume):    Pull queued accounts from server → login here → push tokens back

Usage:
    # Login accounts from file and push to server
    python ryasai_auth.py accounts.txt

    # Push accounts to server queue (someone else will login)
    python ryasai_auth.py accounts.txt --store

    # Pull queued accounts from server, login here, push results back
    python ryasai_auth.py --consume

    # Custom providers
    python ryasai_auth.py accounts.txt --providers kiro,wavespeed

    # Visible browser (for debugging)
    python ryasai_auth.py accounts.txt --headed
"""

from __future__ import annotations

import argparse
import asyncio
import logging
import os
import resource
import signal
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

from config import get_settings

settings = get_settings()

# ── Bump file descriptor limit (browsers need many fds) ──────────
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_NOFILE)
    if soft < 65536:
        resource.setrlimit(resource.RLIMIT_NOFILE, (min(65536, hard), hard))
except (ValueError, OSError):
    pass

# ── Logging ──────────────────────────────────────────────────────
logging.basicConfig(
    level=getattr(logging, settings.log_level.upper(), logging.INFO),
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%H:%M:%S",
)
logger = logging.getLogger("ryasai-auth")

# ── Graceful Shutdown State ──────────────────────────────────────
_running = True
_shutdown_event: asyncio.Event | None = None
_active_tasks: set[asyncio.Task] = set()
_shutdown_start: float = 0
_force_kill_count = 0

GRACEFUL_TIMEOUT = 30  # seconds to wait for tasks to finish before force-kill


def _signal_handler(sig, frame):
    """Handle SIGINT/SIGTERM with graceful shutdown.

    First signal:  initiate graceful shutdown (finish current jobs, flush results)
    Second signal: force immediate exit
    """
    global _running, _force_kill_count, _shutdown_start

    _force_kill_count += 1

    if _force_kill_count == 1:
        _running = False
        _shutdown_start = time.time()
        logger.info("")
        logger.info("⚠️  Graceful shutdown initiated (Ctrl+C again to force-quit)")
        logger.info("   Waiting for active jobs to complete (timeout: %ds)...", GRACEFUL_TIMEOUT)
        # Signal the async event if available
        if _shutdown_event and not _shutdown_event.is_set():
            _shutdown_event.set()
    else:
        logger.warning("🛑 Force shutdown — killing immediately")
        sys.exit(1)


# ═══════════════════════════════════════════════════════════════════
#  ACCOUNT FILE PARSER
# ═══════════════════════════════════════════════════════════════════

def load_accounts(path: str) -> list[dict[str, str]]:
    """Load accounts from a text file.

    Supported formats:
        email:password
        email\\tpassword
        email password
    Lines starting with # are ignored.
    """
    accounts = []
    p = Path(path)
    if not p.exists():
        logger.error("File not found: %s", path)
        sys.exit(1)

    for i, line in enumerate(p.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue

        email, password = None, None

        if ":" in line:
            email, _, password = line.partition(":")
        elif "\t" in line:
            parts = line.split("\t", 1)
            email, password = parts[0], parts[1]
        elif " " in line:
            parts = line.split(None, 1)
            email, password = parts[0], parts[1]

        if email and password:
            email = email.strip()
            password = password.strip()
            if "@" in email:
                accounts.append({"email": email, "password": password})
            else:
                logger.warning("Line %d: invalid email '%s', skipping", i, email)
        else:
            logger.warning("Line %d: could not parse, skipping", i)

    return accounts


# ═══════════════════════════════════════════════════════════════════
#  MODE 1: STORE (push accounts to server queue)
# ═══════════════════════════════════════════════════════════════════

async def store(accounts: list[dict[str, str]], providers: list[str]):
    """Push accounts to server → stored in DB + queued for login.

    The accounts sit in the queue until a worker (server-local or remote)
    consumes them via --consume mode.
    """
    import api_client

    try:
        ok = await api_client.health_check()
        if not ok:
            logger.error("❌ Cannot reach server at %s", settings.server_url)
            return
    except Exception as e:
        logger.error("❌ Server connection failed: %s", e)
        return

    logger.info("📤 Pushing %d accounts to server queue...", len(accounts))

    # Batch in chunks of 50
    total_added = 0
    total_skipped = 0
    total_queued = 0

    for i in range(0, len(accounts), 50):
        chunk = accounts[i:i + 50]
        try:
            result = await api_client.push_accounts(chunk, providers=providers)
            total_added += result.get("added", 0)
            total_skipped += result.get("skipped", 0)
            total_queued += result.get("queued", 0)
            logger.info(
                "  Chunk %d-%d: added=%d, queued=%d",
                i + 1, i + len(chunk),
                result.get("added", 0), result.get("queued", 0),
            )
        except Exception as e:
            logger.error("  Chunk %d-%d failed: %s", i + 1, i + len(chunk), e)

    logger.info(
        "✅ Done: added=%d, skipped=%d, queued=%d (waiting for consumer)",
        total_added, total_skipped, total_queued,
    )
    await api_client.close()


# ═══════════════════════════════════════════════════════════════════
#  MODE 2: CONSUME (pull from server queue → login → push results)
# ═══════════════════════════════════════════════════════════════════

async def consume(
    providers: list[str],
    concurrent: int,
    headless: str,
    proxy_url: str,
    poll_interval: int = 10,
):
    """Pull queued accounts from server, login locally, push results back.

    Runs as a long-lived worker:
      1. GET /api/worker/jobs → pull batch of accounts (plaintext passwords)
      2. Run Camoufox login for each account × provider
      3. POST /api/worker/results → push tokens back to server
      4. Repeat until queue is empty or interrupted

    The server decrypts passwords before sending them to us.
    """
    import api_client
    from login_runner import run_login

    # Check server
    try:
        ok = await api_client.health_check()
        if not ok:
            logger.error("❌ Cannot reach server at %s", settings.server_url)
            return
    except Exception as e:
        logger.error("❌ Server connection failed: %s", e)
        return

    logger.info("🔄 Consumer mode — pulling jobs from server queue...")
    logger.info("   Server: %s", settings.server_url)
    logger.info("   Providers: %s", ", ".join(providers))
    logger.info("   Concurrency: %d", concurrent)
    logger.info("   Poll interval: %ds", poll_interval)
    logger.info("")

    total_success = 0
    total_failed = 0
    empty_polls = 0

    async def _interruptible_sleep(seconds: float):
        """Sleep that can be interrupted by shutdown signal."""
        try:
            await asyncio.wait_for(_shutdown_event.wait(), timeout=seconds)
        except asyncio.TimeoutError:
            pass

    while _running:
        # Send heartbeat
        try:
            await api_client.heartbeat(active_jobs=0)
        except Exception:
            pass

        # Pull jobs from server
        try:
            jobs = await api_client.pull_jobs(
                limit=concurrent * 3,
                providers=",".join(providers),
            )
        except Exception as e:
            logger.error("  Failed to pull jobs: %s (retrying in %ds)", e, poll_interval)
            await _interruptible_sleep(poll_interval)
            continue

        if not jobs:
            empty_polls += 1
            if empty_polls == 1:
                logger.info("  Queue empty — waiting for new accounts...")
            elif empty_polls % 6 == 0:  # Log every ~60s
                logger.info(
                    "  Still waiting... (total: ✅ %d, ❌ %d)",
                    total_success, total_failed,
                )
            await _interruptible_sleep(poll_interval)
            continue

        empty_polls = 0
        logger.info("  📥 Pulled %d jobs from queue", len(jobs))

        # Process jobs: 1 account = 1 browser, sequential with delay
        import random

        account_delay = settings.account_delay
        account_jitter = settings.account_delay_jitter
        results_buffer: list[dict] = []

        for job_idx, job in enumerate(jobs):
            if not _running:
                break

            email = job["email"]
            password = job["password"]
            job_providers = job.get("providers", providers)

            logger.info(
                "  ━━━ [%d/%d] %s ━━━",
                job_idx + 1, len(jobs), email,
            )

            for provider in job_providers:
                if not _running:
                    break

                result = await run_login(
                    email, password, provider,
                    headless=headless, proxy_url=proxy_url,
                )

                if result["status"] == "success":
                    total_success += 1
                    logger.info("  ✅ %s/%s — success", email, provider)
                else:
                    total_failed += 1
                    logger.error(
                        "  ❌ %s/%s — %s",
                        email, provider, result.get("error", "unknown"),
                    )

                # Push result immediately
                try:
                    await api_client.push_result(result)
                except Exception as e:
                    logger.warning("  Failed to push result: %s (buffering)", e)
                    results_buffer.append(result)

            # Anti-ban delay between accounts
            if _running and job_idx < len(jobs) - 1:
                delay = account_delay + random.uniform(0, account_jitter)
                logger.info("  ⏳ Cooling down %.1fs before next account...", delay)
                await _interruptible_sleep(delay)

        # Flush buffered results
        if results_buffer:
            try:
                await api_client.push_results(results_buffer)
                results_buffer.clear()
            except Exception as e:
                logger.error("  Failed to flush %d results: %s", len(results_buffer), e)

        logger.info(
            "  Batch done — total: ✅ %d, ❌ %d",
            total_success, total_failed,
        )

    # Final flush on shutdown
    if results_buffer:
        logger.info("📤 Flushing %d buffered results before exit...", len(results_buffer))
        try:
            await api_client.push_results(results_buffer)
            results_buffer.clear()
            logger.info("  ✅ Flushed successfully")
        except Exception as e:
            logger.error("  ❌ Failed to flush: %s", e)

    logger.info("")
    logger.info("╔══════════════════════════════════════════════════╗")
    logger.info("║  Consumer Session Summary                        ║")
    logger.info("╚══════════════════════════════════════════════════╝")
    logger.info("  ✅ Success:  %d", total_success)
    logger.info("  ❌ Failed:   %d", total_failed)
    logger.info("  📊 Total:    %d", total_success + total_failed)
    if not _running:
        logger.info("  ⚠️  Stopped by shutdown signal")
    logger.info("")

    await api_client.close()


# ═══════════════════════════════════════════════════════════════════
#  MODE 3: LOCAL LOGIN (login here, push results to server)
# ═══════════════════════════════════════════════════════════════════

async def local_login(
    accounts: list[dict[str, str]],
    providers: list[str],
    concurrent: int,
    headless: str,
    proxy_url: str,
):
    """Run browser login locally and push results to the server.

    Two modes controlled by PARALLEL setting:
      - PARALLEL=true:  Run N accounts simultaneously (fast, uses more RAM)
      - PARALLEL=false: Run accounts one-by-one with delay (anti-ban safe)

    Each account always gets its own isolated Camoufox browser instance.
    """
    import random

    import api_client
    from login_runner import run_login

    parallel = settings.parallel
    account_delay = settings.account_delay
    account_jitter = settings.account_delay_jitter

    # Step 1: Check server
    try:
        ok = await api_client.health_check()
        if not ok:
            logger.error("❌ Cannot reach server at %s", settings.server_url)
            return
    except Exception as e:
        logger.error("❌ Server connection failed: %s", e)
        return

    # Step 2: Push accounts to server (store + queue)
    logger.info("📤 Registering %d accounts on server...", len(accounts))
    for i in range(0, len(accounts), 50):
        chunk = accounts[i:i + 50]
        try:
            await api_client.push_accounts(chunk, providers=providers)
        except Exception as e:
            logger.warning("  Failed to register chunk: %s (continuing anyway)", e)

    # Step 3: Run login
    total = len(accounts) * len(providers)
    logger.info(
        "🔑 Starting login: %d accounts × %d providers = %d jobs",
        len(accounts), len(providers), total,
    )

    if parallel:
        logger.info(
            "⚡ PARALLEL mode: %d accounts at once (concurrency=%d)",
            min(concurrent, len(accounts)), concurrent,
        )
    else:
        logger.info(
            "🛡️  SEQUENTIAL mode: 1 account at a time, delay=%.1fs (+jitter %.1fs)",
            account_delay, account_jitter,
        )

    success = 0
    failed = 0
    skipped = 0
    results_buffer: list[dict] = []
    lock = asyncio.Lock()

    async def _login_account(acc_idx: int, acc: dict):
        """Login a single account to all its providers."""
        nonlocal success, failed
        email = acc["email"]
        password = acc["password"]

        logger.info("  🚀 [%d/%d] %s", acc_idx, len(accounts), email)

        for prov in providers:
            if not _running:
                return

            result = await run_login(
                email, password, prov,
                headless=headless, proxy_url=proxy_url,
            )

            async with lock:
                if result["status"] == "success":
                    success += 1
                    logger.info(
                        "  ✅ %s/%s — success (tokens: %s)",
                        email, prov,
                        ", ".join(result.get("tokens", {}).keys()) if result.get("tokens") else "none",
                    )
                else:
                    failed += 1
                    logger.error(
                        "  ❌ %s/%s — %s",
                        email, prov, result.get("error", "unknown"),
                    )

            # Push result to server immediately
            try:
                await api_client.push_result(result)
            except Exception as e:
                logger.warning("  Failed to push result: %s (buffering)", e)
                async with lock:
                    results_buffer.append(result)

    # ── PARALLEL MODE: run N accounts concurrently ───────────────
    if parallel:
        semaphore = asyncio.Semaphore(concurrent)

        async def _throttled_login(idx: int, acc: dict):
            async with semaphore:
                if not _running:
                    return
                await _login_account(idx, acc)

        tasks = [
            asyncio.create_task(_throttled_login(i + 1, acc))
            for i, acc in enumerate(accounts)
        ]
        for t in tasks:
            _active_tasks.add(t)
            t.add_done_callback(_active_tasks.discard)

        # Wait with progress reporting
        done_count = 0
        for coro in asyncio.as_completed(tasks):
            try:
                await coro
            except asyncio.CancelledError:
                skipped += len(providers)
            except Exception as e:
                logger.error("  Unexpected error: %s", e)
            done_count += 1
            if done_count % 5 == 0 or done_count == len(accounts):
                logger.info(
                    "  📊 Progress: %d/%d accounts done (✅ %d, ❌ %d)",
                    done_count, len(accounts), success, failed,
                )

    # ── SEQUENTIAL MODE: 1 account at a time with delay ──────────
    else:
        for acc_idx, acc in enumerate(accounts, 1):
            if not _running:
                skipped += (len(accounts) - acc_idx + 1) * len(providers)
                break

            await _login_account(acc_idx, acc)

            # Progress
            logger.info(
                "  📊 Progress: %d/%d accounts, %d/%d jobs (✅ %d, ❌ %d)",
                acc_idx, len(accounts), success + failed, total, success, failed,
            )

            # Anti-ban delay between accounts
            if _running and acc_idx < len(accounts):
                delay = account_delay + random.uniform(0, account_jitter)
                logger.info("  ⏳ Cooling down %.1fs before next account...", delay)
                try:
                    await asyncio.wait_for(_shutdown_event.wait(), timeout=delay)
                except asyncio.TimeoutError:
                    pass

    # Flush buffered results
    if results_buffer:
        logger.info("📤 Flushing %d buffered results...", len(results_buffer))
        try:
            await api_client.push_results(results_buffer)
            logger.info("  ✅ Flushed successfully")
        except Exception as e:
            logger.error("  ❌ Failed to flush results: %s", e)

    logger.info("")
    logger.info("╔══════════════════════════════════════════════════╗")
    logger.info("║  Session Summary                                 ║")
    logger.info("╚══════════════════════════════════════════════════╝")
    logger.info("  ✅ Success:  %d", success)
    logger.info("  ❌ Failed:   %d", failed)
    if skipped:
        logger.info("  ⏭️  Skipped:  %d (shutdown)", skipped)
    logger.info("  📊 Total:    %d/%d", success + failed, total)
    logger.info("  ⚡ Mode:     %s (concurrency=%d)", "PARALLEL" if parallel else "SEQUENTIAL", concurrent)
    if not _running:
        logger.info("  ⚠️  Stopped early due to shutdown signal")
    logger.info("")

    await api_client.close()


# ═══════════════════════════════════════════════════════════════════
#  MAIN
# ═══════════════════════════════════════════════════════════════════

async def main(args):
    global _shutdown_event
    _shutdown_event = asyncio.Event()

    providers = (
        [p.strip() for p in args.providers.split(",")]
        if args.providers
        else settings.provider_list
    )
    concurrent = args.concurrency or settings.concurrency
    headless = "false" if args.headed else settings.camoufox_headless
    proxy_url = args.proxy or settings.proxy_url

    logger.info("╔══════════════════════════════════════════════════╗")
    logger.info("║  ryasai-auth — Standalone Auth Agent             ║")
    logger.info("╚══════════════════════════════════════════════════╝")
    logger.info("  Server:     %s", settings.server_url)
    logger.info("  Providers:  %s", ", ".join(providers))

    # ── CONSUME MODE: no accounts file needed ────────────────────
    if args.consume:
        logger.info("  Mode:       consume (pull from server queue)")
        logger.info("  Parallel:   %s", "⚡ YES" if settings.parallel else "🛡️ NO (sequential)")
        logger.info("  Concurrency: %d", concurrent)
        logger.info("  Poll:       %ds", args.poll_interval)
        logger.info("")
        await consume(providers, concurrent, headless, proxy_url, args.poll_interval)
        return

    # ── STORE / LOCAL LOGIN: accounts file required ──────────────
    if not args.accounts_file:
        logger.error("accounts_file is required for --store and local-login modes")
        sys.exit(1)

    accounts = load_accounts(args.accounts_file)
    if not accounts:
        logger.error("No valid accounts found in %s", args.accounts_file)
        sys.exit(1)

    logger.info("  Accounts:   %d", len(accounts))

    if args.store:
        logger.info("  Mode:       store (push to server queue)")
        logger.info("")
        await store(accounts, providers)
    else:
        logger.info("  Mode:       local-login (login here, push tokens)")
        logger.info("  Parallel:   %s", "⚡ YES" if settings.parallel else "🛡️ NO (sequential)")
        logger.info("  Concurrency: %d", concurrent)
        if not settings.parallel:
            logger.info("  Delay:      %.1fs + jitter %.1fs", settings.account_delay, settings.account_delay_jitter)
        logger.info("")
        await local_login(accounts, providers, concurrent, headless, proxy_url)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="ryasai-auth — Login accounts and push tokens to ryasai server",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Modes:
  LOCAL LOGIN (default):
    python ryasai_auth.py accounts.txt
    → Login on this PC, push tokens to server

  STORE:
    python ryasai_auth.py accounts.txt --store
    → Push accounts to server queue (no login here)

  CONSUME:
    python ryasai_auth.py --consume
    → Pull queued accounts from server, login here, push tokens back

Examples:
  python ryasai_auth.py accounts.txt
  python ryasai_auth.py accounts.txt --store
  python ryasai_auth.py --consume
  python ryasai_auth.py --consume --concurrency 5
  python ryasai_auth.py accounts.txt --providers kiro,wavespeed
  python ryasai_auth.py accounts.txt --headed
  python ryasai_auth.py --consume --proxy socks5://user:pass@host:port

Accounts file format (one per line):
  email@example.com:password123
  email2@example.com:hunter2
  # Lines starting with # are ignored
        """,
    )

    parser.add_argument(
        "accounts_file",
        nargs="?",
        default=None,
        help="Path to accounts file (email:password per line). Required for --store and local-login.",
    )

    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--store",
        action="store_true",
        help="Push accounts to server queue (no login on this machine)",
    )
    mode.add_argument(
        "--consume",
        action="store_true",
        help="Pull queued accounts from server, login here, push results back",
    )

    parser.add_argument("--providers", type=str, help="Comma-separated providers (default: from .env)")
    parser.add_argument("--concurrency", type=int, help="Max concurrent logins (default: from .env)")
    parser.add_argument("--headed", action="store_true", help="Show browser window (for debugging)")
    parser.add_argument("--proxy", type=str, help="Proxy URL for browser sessions")
    parser.add_argument(
        "--poll-interval", type=int, default=10,
        help="Seconds between queue polls in --consume mode (default: 10)",
    )

    args = parser.parse_args()

    signal.signal(signal.SIGINT, _signal_handler)
    signal.signal(signal.SIGTERM, _signal_handler)

    asyncio.run(main(args))
