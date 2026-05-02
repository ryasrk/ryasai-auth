"""Login runner — executes browser-based login for a single account.

Reuses the existing provider adapters (kiro, codebuddy, wavespeed, canva, yepapi).
Returns a result dict ready to be pushed to the server.
"""

from __future__ import annotations

import asyncio
import logging
import os
from datetime import datetime, timezone
from typing import Any

from providers.base import NormalizedAccount, ProviderResult
from errors.exceptions import BatcherError, RetryableBatcherError

logger = logging.getLogger("ryasai-auth.login")

MAX_RETRIES = 3
BASE_DELAY = 2.0
MAX_DELAY = 15.0


def _get_adapter(provider: str):
    """Get the provider adapter by name."""
    provider = provider.lower()
    if provider == "kiro":
        from providers.kiro import KiroProviderAdapter
        return KiroProviderAdapter()
    elif provider == "codebuddy":
        from providers.codebuddy import CodeBuddyProviderAdapter
        return CodeBuddyProviderAdapter()
    elif provider == "wavespeed":
        from providers.wavespeed import WavespeedProviderAdapter
        return WavespeedProviderAdapter()
    elif provider == "canva":
        from providers.canva import CanvaProviderAdapter
        return CanvaProviderAdapter()
    elif provider == "yepapi":
        from providers.yepapi import YepAPIAdapter
        return YepAPIAdapter()
    else:
        raise ValueError(f"Unknown provider: {provider}")


async def run_login(
    email: str,
    password: str,
    provider: str,
    *,
    headless: str = "true",
    proxy_url: str = "",
) -> dict[str, Any]:
    """Run browser login for a single email/provider combo.

    Returns a result dict:
        {
            "email": "...",
            "provider": "...",
            "status": "success" | "failed",
            "tokens": {...} | None,
            "quota": {...} | None,
            "error": "..." | None,
            "worker_id": "...",
            "timestamp": "...",
        }
    """
    from config import get_settings
    settings = get_settings()

    # Set browser env — MUST enable Camoufox for real browser login
    os.environ["BATCHER_ENABLE_CAMOUFOX"] = "true"
    os.environ["BATCHER_CAMOUFOX_HEADLESS"] = headless
    if proxy_url:
        os.environ["BATCHER_PROXY_URL"] = proxy_url

    adapter = _get_adapter(provider)
    account = NormalizedAccount(
        provider=provider,
        identifier=email,
        secret=password,
        raw=f"{email}:{password}",
    )

    last_error = ""

    for attempt in range(MAX_RETRIES):
        session = None
        try:
            session = await adapter.bootstrap_session(account)
            logger.info(
                "  [%s] %s — browser ready (attempt %d/%d)",
                provider, email, attempt + 1, MAX_RETRIES,
            )

            auth_state = await adapter.authenticate(account, session)
            logger.info("  [%s] %s — authenticated", provider, email)

            tokens = await adapter.fetch_tokens(account, auth_state, session)
            logger.info("  [%s] %s — tokens obtained", provider, email)

            quota = None
            try:
                quota = await adapter.fetch_quota(account, tokens, session)
            except Exception as qe:
                logger.debug("  [%s] %s — quota fetch failed: %s", provider, email, qe)

            return {
                "email": email,
                "provider": provider,
                "status": "success",
                "tokens": tokens,
                "quota": quota,
                "error": None,
                "worker_id": settings.worker_id,
                "timestamp": datetime.now(timezone.utc).isoformat(),
            }

        except RetryableBatcherError as e:
            last_error = f"{e.code.value}: {e.message}"
            delay = min(BASE_DELAY * (2 ** attempt), MAX_DELAY)
            logger.warning(
                "  [%s] %s — retryable error: %s (retry in %.1fs)",
                provider, email, last_error, delay,
            )
            await asyncio.sleep(delay)

        except BatcherError as e:
            last_error = f"{e.code.value}: {e.message}"
            logger.error("  [%s] %s — non-retryable: %s", provider, email, last_error)
            break

        except Exception as e:
            last_error = str(e)
            delay = min(BASE_DELAY * (2 ** attempt), MAX_DELAY)
            logger.warning(
                "  [%s] %s — error: %s (retry in %.1fs)",
                provider, email, last_error, delay,
            )
            await asyncio.sleep(delay)

        finally:
            if session:
                try:
                    await adapter.cleanup_session(session)
                except Exception:
                    pass

    return {
        "email": email,
        "provider": provider,
        "status": "failed",
        "tokens": None,
        "quota": None,
        "error": last_error,
        "worker_id": settings.worker_id,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }
