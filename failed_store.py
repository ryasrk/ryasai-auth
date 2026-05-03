"""Local persistent store for failed login accounts.

Saves failed accounts to a JSON file so they can be retried later
with `ryasai_auth.py --retry`.

File: ~/.ryasai-auth/failed_accounts.json
Format:
  [
    {
      "email": "user@example.com",
      "password": "...",
      "provider": "kiro",
      "error": "AUTH_FAILED: invalid credentials",
      "failed_at": "2025-06-01T12:00:00+00:00",
      "attempts": 1
    },
    ...
  ]

Thread-safe via asyncio.Lock.
"""

from __future__ import annotations

import json
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

logger = logging.getLogger("ryasai-auth.failed")

# ── Storage location ─────────────────────────────────────────────
STORE_DIR = Path.home() / ".ryasai-auth"
STORE_FILE = STORE_DIR / "failed_accounts.json"

_lock = None  # initialized lazily per event loop


def _get_lock():
    """Lazy-init asyncio lock (must be created inside running loop)."""
    global _lock
    import asyncio
    if _lock is None:
        _lock = asyncio.Lock()
    return _lock


def _ensure_dir():
    """Create storage directory if it doesn't exist."""
    STORE_DIR.mkdir(parents=True, exist_ok=True)


def _load_raw() -> list[dict[str, Any]]:
    """Load the raw JSON list from disk (sync)."""
    if not STORE_FILE.exists():
        return []
    try:
        data = json.loads(STORE_FILE.read_text(encoding="utf-8"))
        return data if isinstance(data, list) else []
    except (json.JSONDecodeError, OSError) as e:
        logger.warning("Failed to read %s: %s", STORE_FILE, e)
        return []


def _save_raw(entries: list[dict[str, Any]]):
    """Write the full list back to disk (sync)."""
    _ensure_dir()
    STORE_FILE.write_text(
        json.dumps(entries, indent=2, ensure_ascii=False),
        encoding="utf-8",
    )


# ═══════════════════════════════════════════════════════════════════
#  PUBLIC API
# ═══════════════════════════════════════════════════════════════════

async def save_failed(
    email: str,
    password: str,
    provider: str,
    error: str,
):
    """Append a failed account to the local store.

    If the same email+provider already exists, increment attempts
    and update the error/timestamp instead of duplicating.
    """
    async with _get_lock():
        entries = _load_raw()

        # Check for existing entry (same email + provider)
        for entry in entries:
            if entry["email"] == email and entry["provider"] == provider:
                entry["error"] = error
                entry["failed_at"] = datetime.now(timezone.utc).isoformat()
                entry["attempts"] = entry.get("attempts", 1) + 1
                # Update password in case it was changed
                entry["password"] = password
                _save_raw(entries)
                logger.debug(
                    "Updated failed entry: %s/%s (attempt #%d)",
                    email, provider, entry["attempts"],
                )
                return

        # New entry
        entries.append({
            "email": email,
            "password": password,
            "provider": provider,
            "error": error,
            "failed_at": datetime.now(timezone.utc).isoformat(),
            "attempts": 1,
        })
        _save_raw(entries)
        logger.debug("Saved failed account: %s/%s", email, provider)


async def save_failed_from_result(result: dict, password: str):
    """Save a failed account from a login result dict.

    Convenience wrapper — extracts email/provider/error from the
    result dict returned by login_runner.run_login().
    """
    if result.get("status") != "failed":
        return

    await save_failed(
        email=result["email"],
        password=password,
        provider=result["provider"],
        error=result.get("error", "unknown"),
    )


async def remove_succeeded(email: str, provider: str):
    """Remove an entry from the failed store after successful retry."""
    async with _get_lock():
        entries = _load_raw()
        before = len(entries)
        entries = [
            e for e in entries
            if not (e["email"] == email and e["provider"] == provider)
        ]
        if len(entries) < before:
            _save_raw(entries)
            logger.debug("Removed from failed store: %s/%s", email, provider)


async def load_failed() -> list[dict[str, Any]]:
    """Load all failed accounts from the store.

    Returns list of dicts with: email, password, provider, error, failed_at, attempts
    """
    async with _get_lock():
        return _load_raw()


async def clear_all():
    """Clear the entire failed store."""
    async with _get_lock():
        if STORE_FILE.exists():
            STORE_FILE.unlink()
            logger.info("Cleared failed accounts store")


def count_sync() -> int:
    """Synchronous count of failed entries (for display at startup)."""
    return len(_load_raw())


def get_store_path() -> Path:
    """Return the path to the store file."""
    return STORE_FILE
