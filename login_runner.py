"""Login runner — executes browser-based login for a single account.

Reuses the existing provider adapters (kiro, codebuddy, wavespeed, canva, yepapi).
Returns a result dict ready to be pushed to the server.
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from providers.base import NormalizedAccount, ProviderResult
from errors.exceptions import BatcherError, RetryableBatcherError

logger = logging.getLogger("ryasai-auth.login")

MAX_RETRIES = 3
BASE_DELAY = 2.0
MAX_DELAY = 15.0

ROD_BINARY = Path(__file__).parent / "bin" / "rod-login"

# Provider → target URL that triggers Google OAuth
ROD_TARGET_URLS = {
    "wavespeed": "https://wavespeed.ai/center/default/google/login?redirect=https://wavespeed.ai/",
    "kiro": "https://kiro.dev/login",
    "codebuddy": "https://www.codebuddy.ai/login",
    "canva": "https://www.canva.com/login",
}


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
    browser_backend: str = "camoufox",
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
    if browser_backend == "rod" and provider in ROD_TARGET_URLS:
        return await _run_rod_login(email, password, provider, headless=headless, proxy_url=proxy_url)

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


async def _run_rod_login(
    email: str,
    password: str,
    provider: str,
    *,
    headless: str = "true",
    proxy_url: str = "",
) -> dict[str, Any]:
    """Run login via go-rod subprocess."""
    from config import get_settings
    settings = get_settings()

    if not ROD_BINARY.exists():
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": f"rod binary not found at {ROD_BINARY}. Run: cd rod && bash build.sh",
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    target_url = ROD_TARGET_URLS.get(provider, "")
    if not target_url:
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": f"No rod target URL configured for provider: {provider}",
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    request_payload = json.dumps({
        "email": email,
        "password": password,
        "provider": provider,
        "target_url": target_url,
        "headless": headless.lower() == "true",
        "proxy": proxy_url,
    })

    try:
        proc = await asyncio.create_subprocess_exec(
            str(ROD_BINARY),
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(
            proc.communicate(input=request_payload.encode()),
            timeout=120,
        )
    except asyncio.TimeoutError:
        proc.kill()
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": "rod subprocess timed out (120s)",
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }
    except Exception as e:
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": f"rod subprocess error: {e}",
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    # Always log stderr — rod region/redirect/consent logs are critical
    if stderr:
        for line in stderr.decode().strip().splitlines():
            logger.info("  [rod/%s] %s", provider, line)

    if proc.returncode != 0:
        err_msg = stderr.decode().strip() or f"rod exited with code {proc.returncode}"
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": err_msg,
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    try:
        rod_result = json.loads(stdout.decode().strip())
    except json.JSONDecodeError as e:
        return {
            "email": email,
            "provider": provider,
            "status": "failed",
            "tokens": None,
            "quota": None,
            "error": f"rod returned invalid JSON: {e}",
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    if rod_result.get("status") == "success":
        cookies = rod_result.get("cookies", {})

        # Provider-specific post-processing: exchange cookies for API key
        if provider == "codebuddy" and cookies:
            api_key = await _rod_codebuddy_get_api_key(cookies)
            if api_key:
                return {
                    "email": email,
                    "provider": provider,
                    "status": "success",
                    "tokens": {"api_key": api_key},
                    "quota": {
                        "credit_capacity_size": 250,
                        "credit_capacity_used": 0,
                        "credit_capacity_remain": 250,
                        "credit_total_dosage": 250,
                    },
                    "error": None,
                    "worker_id": settings.worker_id,
                    "timestamp": datetime.now(timezone.utc).isoformat(),
                }
            else:
                return {
                    "email": email,
                    "provider": provider,
                    "status": "failed",
                    "tokens": None,
                    "quota": None,
                    "error": "rod login succeeded but failed to create API key from cookies",
                    "worker_id": settings.worker_id,
                    "timestamp": datetime.now(timezone.utc).isoformat(),
                }

        return {
            "email": email,
            "provider": provider,
            "status": "success",
            "tokens": cookies,
            "quota": None,
            "error": None,
            "worker_id": settings.worker_id,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }

    return {
        "email": email,
        "provider": provider,
        "status": "failed",
        "tokens": None,
        "quota": None,
        "error": rod_result.get("error", "unknown rod error"),
        "worker_id": settings.worker_id,
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


async def _rod_codebuddy_get_api_key(cookies: dict[str, str]) -> str | None:
    """After rod login succeeds, use cookies to create a CodeBuddy API key.

    Steps:
      1. Set region (Singapore) via /console/login/account
      2. Get userEnterpriseId via /console/accounts
      3. Create API key via /console/api/client/v1/api-keys
    """
    import time
    import aiohttp

    base_url = os.environ.get("BATCHER_CODEBUDDY_BASE_URL", "https://www.codebuddy.ai")
    cookie_header = "; ".join(f"{k}={v}" for k, v in cookies.items())

    headers = {
        "Cookie": cookie_header,
        "Content-Type": "application/json",
        "Accept": "application/json, text/plain, */*",
        "X-Requested-With": "XMLHttpRequest",
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
        "Origin": base_url,
        "Referer": f"{base_url}/",
    }

    timeout = aiohttp.ClientTimeout(total=20)

    try:
        async with aiohttp.ClientSession(timeout=timeout, headers=headers) as client:
            # Step 1: Set region (Singapore)
            region_payload = {
                "attributes": {
                    "countryCode": ["65"],
                    "countryFullName": ["Singapore"],
                    "countryName": ["SG"],
                }
            }
            try:
                async with client.post(
                    f"{base_url}/console/login/account", json=region_payload
                ) as resp:
                    logger.debug("  [rod/codebuddy] set region status=%d", resp.status)
            except Exception as e:
                logger.debug("  [rod/codebuddy] set region failed: %s", e)

            # Step 2: Get userEnterpriseId
            user_enterprise_id = "personal-edition-user-id"
            try:
                async with client.get(f"{base_url}/console/accounts") as resp:
                    if resp.status == 200:
                        data = await resp.json()
                        accounts = (data.get("data") or {}).get("accounts") or []
                        if accounts:
                            user_enterprise_id = str(
                                accounts[0].get("userEnterpriseId") or user_enterprise_id
                            )
                        logger.debug("  [rod/codebuddy] enterprise_id=%s", user_enterprise_id)
            except Exception as e:
                logger.debug("  [rod/codebuddy] get accounts failed: %s", e)

            # Step 3: Create API key
            timestamp = int(time.time())
            key_payload = {
                "name": f"rod-{timestamp}",
                "expire_in_days": -1,
                "user_enterprise_id": user_enterprise_id,
            }
            async with client.post(
                f"{base_url}/console/api/client/v1/api-keys", json=key_payload
            ) as resp:
                if resp.status != 200:
                    body = await resp.text()
                    logger.warning("  [rod/codebuddy] create API key failed: status=%d body=%s", resp.status, body[:150])
                    return None

                payload = await resp.json()
                if payload.get("code") != 0:
                    logger.warning("  [rod/codebuddy] create API key error code=%s", payload.get("code"))
                    return None

                api_key = str((payload.get("data") or {}).get("key") or "").strip()
                if api_key:
                    logger.info("  [rod/codebuddy] API key created: %s...", api_key[:20])
                    return api_key

                return None

    except Exception as e:
        logger.warning("  [rod/codebuddy] API key creation error: %s", e)
        return None
