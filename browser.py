"""Shared Camoufox browser configuration for all providers.

Centralises browser kwargs so every provider uses the same optimised
settings.  Import ``build_camoufox_kwargs`` in each provider instead of
duplicating the dict literal.

Performance optimisations applied (based on Camoufox research):
  • block_images=True   – skip image downloads → faster loads, less bandwidth
  • headless="virtual"  – Xvfb virtual display on Linux for better stealth
  • block_webrtc=True   – prevent WebRTC IP leaks
  • enable_cache=False  – don't accumulate page cache (saves memory in batch)
"""

from __future__ import annotations

import os
import sys
from typing import Any


def _resolve_headless() -> bool | str:
    """Return the best headless mode for the current environment.

    Priority:
      1. BATCHER_CAMOUFOX_HEADLESS=false  → headed (False)
      2. BATCHER_CAMOUFOX_HEADLESS=virtual → Xvfb virtual display ("virtual")
      3. Linux + Xvfb available            → "virtual" (best stealth)
      4. Fallback                          → True (native headless)
    """
    env = os.getenv("BATCHER_CAMOUFOX_HEADLESS", "true").lower().strip()

    if env == "false":
        return False
    if env == "virtual":
        return "virtual"

    # On Linux, prefer virtual display for better anti-detection stealth
    if sys.platform == "linux" and _xvfb_available():
        return "virtual"

    return True


def _xvfb_available() -> bool:
    """Check whether Xvfb is installed."""
    import shutil
    return shutil.which("Xvfb") is not None


def build_camoufox_kwargs(
    *,
    extra: dict[str, Any] | None = None,
    proxy_env_var: str = "BATCHER_PROXY_URL",
) -> dict[str, Any]:
    """Build the standard ``AsyncCamoufox(**kwargs)`` dict.

    Parameters
    ----------
    extra:
        Provider-specific overrides merged *on top* of the defaults.
        e.g. ``{"disable_coop": True}`` for CodeBuddy.
    proxy_env_var:
        Name of the env-var holding the proxy URL.  Defaults to
        ``BATCHER_PROXY_URL``; codex_login uses ``CODEX_PROXY_URL``.

    Returns
    -------
    dict ready to be unpacked into ``AsyncCamoufox(**kwargs)``.
    """
    from browserforge.fingerprints import Screen

    kwargs: dict[str, Any] = {
        "headless": _resolve_headless(),
        "os": "windows",
        "block_images": True,       # ← perf: skip image downloads
        "block_webrtc": True,       # ← stealth: prevent IP leaks
        "enable_cache": False,      # ← memory: don't accumulate cache
        "humanize": False,          # ← speed: no cursor animation
        "screen": Screen(max_width=1920, max_height=1080),
    }

    # Proxy + GeoIP
    proxy_url = os.getenv(proxy_env_var, "")
    if proxy_url:
        from urllib.parse import urlparse

        parsed = urlparse(proxy_url)
        proxy_cfg: dict[str, Any] = {
            "server": f"{parsed.scheme}://{parsed.hostname}:{parsed.port}"
        }
        if parsed.username:
            proxy_cfg["username"] = parsed.username
        if parsed.password:
            proxy_cfg["password"] = parsed.password
        kwargs["proxy"] = proxy_cfg
        kwargs["geoip"] = True

    # Merge provider-specific overrides
    if extra:
        kwargs.update(extra)

    return kwargs
