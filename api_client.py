"""HTTP client for communicating with the ryasai server.

Handles:
  - Pushing accounts to the server (store + optional auto-login)
  - Pushing login results (tokens) back to the server
  - Heartbeat
  - Checking server health
"""

from __future__ import annotations

import logging
from typing import Any

import httpx

from config import get_settings

logger = logging.getLogger("ryasai-auth.api")

settings = get_settings()

_client: httpx.AsyncClient | None = None


def _get_client() -> httpx.AsyncClient:
    """Lazy-init persistent HTTP client."""
    global _client
    if _client is None or _client.is_closed:
        headers = {"Content-Type": "application/json"}
        if settings.server_admin_key:
            headers["Authorization"] = f"Bearer {settings.server_admin_key}"
        _client = httpx.AsyncClient(
            base_url=settings.server_url,
            headers=headers,
            timeout=30.0,
        )
    return _client


async def close() -> None:
    """Close the HTTP client."""
    global _client
    if _client and not _client.is_closed:
        await _client.aclose()
        _client = None


async def health_check() -> bool:
    """Check if the server is reachable."""
    client = _get_client()
    try:
        resp = await client.get("/api/health")
        return resp.status_code == 200
    except Exception:
        return False


async def push_accounts(
    accounts: list[dict[str, str]],
    *,
    providers: list[str] | None = None,
    concurrent: int = 2,
) -> dict[str, Any]:
    """Push accounts to the server (stored in DB + queued for login).

    All accounts go to POST /api/worker/accounts which now always
    stores AND queues. A consumer (server-local or remote --consume)
    will pick them up.
    """
    client = _get_client()
    payload: dict[str, Any] = {
        "accounts": accounts,
        "concurrent": concurrent,
    }
    if providers:
        payload["providers"] = providers

    resp = await client.post("/api/worker/accounts", json=payload)
    resp.raise_for_status()
    return resp.json()


async def pull_jobs(
    limit: int = 10,
    providers: str | None = None,
) -> list[dict[str, Any]]:
    """Pull queued login jobs from the server.

    Server decrypts passwords before sending — we receive plaintext.

    Returns:
        List of job dicts: [{"email": "...", "password": "...", "providers": [...], ...}]
    """
    client = _get_client()
    params: dict[str, Any] = {"limit": limit}
    if providers:
        params["providers"] = providers

    resp = await client.get("/api/worker/jobs", params=params)
    resp.raise_for_status()
    data = resp.json()
    return data.get("jobs", [])


async def push_result(result: dict) -> dict[str, Any]:
    """Push a single login result to the server."""
    return await push_results([result])


async def push_results(results: list[dict]) -> dict[str, Any]:
    """Push login results to the server."""
    client = _get_client()
    resp = await client.post("/api/worker/results", json={"results": results})
    resp.raise_for_status()
    return resp.json()


async def heartbeat(active_jobs: int = 0) -> bool:
    """Send heartbeat to the server."""
    client = _get_client()
    try:
        resp = await client.post("/api/worker/heartbeat", json={
            "worker_id": settings.worker_id,
            "concurrency": settings.concurrency,
            "active_jobs": active_jobs,
            "version": "1.0.0",
        })
        return resp.status_code == 200
    except Exception:
        return False
