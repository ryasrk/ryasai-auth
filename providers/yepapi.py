import asyncio
import json
import os
import ssl
import time
from typing import Any
from urllib.parse import urlparse

import aiohttp

_SSL_CTX = ssl.create_default_context()
_SSL_CTX.check_hostname = False
_SSL_CTX.verify_mode = ssl.CERT_NONE

from errors.codes import ErrorCode
from errors.exceptions import NonRetryableBatcherError, RetryableBatcherError
from providers.base import NormalizedAccount, ProviderAdapter

YEPAPI_LOGIN_URL = "https://www.yepapi.com/login"
YEPAPI_KEYGEN_URL = "https://www.yepapi.com/dashboard/api-keys"
YEPAPI_KEYGEN_ACTION = "4049ed4a5ef885cfe9938aac3cd35c037b2d04e1b8"


def _debug(msg: str) -> None:
    if os.getenv("BATCHER_YEPAPI_DEBUG", "false").lower() == "true":
        print(f"[yepapi-auth] {msg}", flush=True)


async def _wait_for_google_email_transition(target: Any) -> bool:
    try:
        await target.wait_for_function(
            """() => {
                const host = window.location.host || '';
                const visible = (sels) => sels.some(s => Array.from(document.querySelectorAll(s)).some(e => e.offsetParent !== null));
                const hasEmail = visible(['#identifierId', 'input[name="identifier"]', 'input[type="email"]']);
                const hasPassword = visible(['input[name="Passwd"]', 'input[type="password"]']);
                if (!host.includes('accounts.google.com')) return true;
                if (hasPassword) return true;
                return !hasEmail;
            }""",
            timeout=10000,
        )
        return True
    except Exception:
        return False


async def _wait_for_google_password_transition(target: Any) -> bool:
    try:
        await target.wait_for_function(
            """() => {
                const host = window.location.host || '';
                const path = window.location.pathname || '';
                const hasPassword = Array.from(document.querySelectorAll('input[name="Passwd"], input[type="password"]')).some(e => e.offsetParent !== null);
                if (!host.includes('accounts.google.com')) return true;
                if (!path.includes('/challenge/pwd')) return true;
                return !hasPassword;
            }""",
            timeout=12000,
        )
        return True
    except Exception:
        return False


async def _is_email_step(page: Any) -> bool:
    try:
        return bool(await page.evaluate("""() => {
            for (const el of document.querySelectorAll('#identifierId, input[type="email"], input[name="identifier"]')) {
                if (el.offsetParent !== null) return true;
            }
            return false;
        }"""))
    except Exception:
        return False


async def _is_password_step(page: Any) -> bool:
    try:
        return bool(await page.evaluate("""() => {
            for (const el of document.querySelectorAll('input[name="Passwd"], input[type="password"]')) {
                if (el.offsetParent !== null) return true;
            }
            return false;
        }"""))
    except Exception:
        return False


async def _click_google_next(page: Any) -> bool:
    try:
        return bool(await page.evaluate("""() => {
            const btn = document.querySelector('#identifierNext button, #passwordNext button');
            if (btn && btn.offsetParent !== null) { btn.click(); return true; }
            for (const el of document.querySelectorAll('div.VfPpkd-RLmnJb, button, div[role="button"]')) {
                const p = el.closest('button, div[role="button"]') || el;
                if (p && p.offsetParent !== null) { p.click(); return true; }
            }
            return false;
        }"""))
    except Exception:
        return False


async def _fill_google_email(page: Any, email: str) -> bool:
    try:
        await page.wait_for_selector("#identifierId", state="visible", timeout=3000)
    except Exception:
        pass
    loc = page.locator("#identifierId").first
    try:
        if await loc.count() == 0 or not await loc.is_visible():
            return False
        await loc.scroll_into_view_if_needed()
        await loc.click(force=True)
        await asyncio.sleep(0.2)
        await loc.press("Control+a")
        await loc.press("Backspace")
        await loc.press_sequentially(email, delay=60)
        await asyncio.sleep(0.5)
        val = await loc.input_value()
        if email.lower() != str(val).lower().strip():
            return False
        if not await _click_google_next(page):
            await loc.press("Enter")
        await _wait_for_google_email_transition(page)
        return True
    except Exception:
        return False


async def _fill_google_password(page: Any, password: str) -> bool:
    for selector in ['input[name="Passwd"]', 'input[type="password"]']:
        try:
            await page.wait_for_selector(selector, state="visible", timeout=3000)
        except Exception:
            pass
        loc = page.locator(selector).first
        try:
            if await loc.count() == 0 or not await loc.is_visible():
                continue
            await loc.scroll_into_view_if_needed()
            await loc.click(force=True)
            await asyncio.sleep(0.2)
            await loc.press("Control+a")
            await loc.press("Backspace")
            await loc.press_sequentially(password, delay=70)
            await asyncio.sleep(0.5)
            if not await _click_google_next(page):
                await loc.press("Enter")
            await _wait_for_google_password_transition(page)
            return True
        except Exception:
            continue
    return False


async def _handle_gaplustos(page: Any) -> bool:
    try:
        url = page.url
    except Exception:
        return False
    if "/speedbump/gaplustos" not in url:
        return False
    try:
        await page.wait_for_selector('#confirm, input[name="confirm"], input[type="submit"]', state="visible", timeout=5000)
    except Exception:
        pass
    for sel in ["#confirm", 'input[name="confirm"]', 'input[type="submit"]']:
        loc = page.locator(sel).first
        try:
            if await loc.count() > 0 and await loc.is_visible():
                await loc.click(force=True)
                return True
        except Exception:
            continue
    try:
        return bool(await page.evaluate("""() => {
            for (const el of document.querySelectorAll('input[type="submit"], button')) {
                if (!el || el.offsetParent === null) continue;
                const txt = (el.value || el.textContent || '').toLowerCase();
                if (txt.includes('agree') || txt.includes('understand') || txt.includes('confirm') || txt.includes('mengerti')) {
                    el.click(); return true;
                }
            }
            return false;
        }"""))
    except Exception:
        return False


async def _handle_consent(page: Any) -> bool:
    try:
        url = page.url
    except Exception:
        return False
    if "accounts.google.com" not in url:
        return False
    if "/signin/oauth" not in url and "/consent" not in url:
        return False
    try:
        clicked = await page.evaluate("""() => {
            for (const btn of document.querySelectorAll('button, div[role="button"]')) {
                const txt = (btn.textContent || '').trim().toLowerCase();
                if (!txt || btn.offsetParent === null) continue;
                if (txt === 'continue' || txt.includes('allow') || txt.includes('lanjut')) {
                    btn.click(); return true;
                }
            }
            const submits = document.querySelectorAll('input[type="submit"], button[type="submit"]');
            for (const btn of submits) {
                if (btn.offsetParent !== null) { btn.click(); return true; }
            }
            return false;
        }""")
        if clicked:
            await asyncio.sleep(2)
        return bool(clicked)
    except Exception:
        return False


async def _click_continue(page: Any) -> None:
    try:
        await page.evaluate("""() => {
            const kw = ['next','continue','accept','i understand','agree','ok','got it','login','sign in'];
            for (const btn of document.querySelectorAll('button, div[role="button"], input[type="submit"]')) {
                const txt = (btn.textContent || btn.value || '').toLowerCase().trim();
                if (txt && kw.some(k => txt.includes(k)) && btn.offsetParent !== null) { btn.click(); return; }
            }
        }""")
    except Exception:
        pass


class YepAPIAdapter(ProviderAdapter):
    name = "yepapi"

    async def parse_account(self, raw_line: str) -> NormalizedAccount:
        parts = raw_line.strip().split(":", 1)
        if len(parts) < 2:
            parts = raw_line.strip().split("|", 1)
        if len(parts) < 2:
            raise NonRetryableBatcherError(ErrorCode.invalid_credentials, "expected email:password or email|password")
        return NormalizedAccount(provider=self.name, identifier=parts[0].strip(), secret=parts[1].strip())

    async def bootstrap_session(self, account: NormalizedAccount) -> Any:
        if os.getenv("BATCHER_ENABLE_CAMOUFOX", "false").lower() != "true":
            return {"stub": True}

        try:
            from camoufox.async_api import AsyncCamoufox

            from browser import build_camoufox_kwargs

            # Disable block_images — Cloudflare WAF flags it as bot behavior
            camoufox_kwargs = build_camoufox_kwargs(
                extra={"block_images": False, "i_know_what_im_doing": True},
            )
            manager = AsyncCamoufox(**camoufox_kwargs)
            browser = await manager.__aenter__()
            page = await browser.new_page()
            page.set_default_timeout(30000)

            await page.goto(YEPAPI_LOGIN_URL, wait_until="domcontentloaded", timeout=30000)

            # Wait for Cloudflare challenge to pass (up to 30s)
            for _ in range(15):
                title = await page.title()
                if "just a moment" not in title.lower():
                    break
                await asyncio.sleep(2)

            await asyncio.sleep(2)
            await page.locator('button:has-text("Google")').first.click(timeout=15000)
            await asyncio.sleep(3)

            return {"stub": False, "manager": manager, "browser": browser, "page": page}
        except Exception as exc:
            raise RetryableBatcherError(ErrorCode.browser_start_failed, str(exc) or "camoufox bootstrap failed") from exc

    async def authenticate(self, account: NormalizedAccount, session: Any) -> dict[str, Any]:
        if session is None or session.get("stub"):
            return {"authenticated": True}

        page = session.get("page")
        if page is None:
            raise RetryableBatcherError(ErrorCode.browser_unexpected_state, "missing browser page")

        email_deadline = 0.0
        pw_deadline = 0.0

        for _ in range(90):
            try:
                url = page.url
            except Exception:
                raise RetryableBatcherError(ErrorCode.browser_unexpected_state, "page lost")

            host = urlparse(url).netloc
            path = urlparse(url).path

            if "yepapi.com" in host and "/dashboard" in path:
                _debug("reached yepapi dashboard")
                cookies = await page.context.cookies()
                cookie_parts = [f'{c["name"]}={c["value"]}' for c in cookies if "yepapi" in c.get("domain", "")]
                return {"authenticated": True, "cookies": "; ".join(cookie_parts)}

            if "SetSID" in url or "/accounts/set" in url.lower():
                await asyncio.sleep(0.5)
                continue

            if await _handle_gaplustos(page):
                _debug("accepted Google TOS")
                await asyncio.sleep(0.8)
                continue

            if await _handle_consent(page):
                _debug("accepted OAuth consent")
                await asyncio.sleep(0.8)
                continue

            if "accounts.google.com" in host:
                now = time.monotonic()
                at_pw = await _is_password_step(page)
                at_email = await _is_email_step(page)

                if at_email and not at_pw:
                    if now < email_deadline:
                        await asyncio.sleep(0.4)
                        continue
                    if await _fill_google_email(page, account.identifier):
                        _debug("entered email")
                        email_deadline = time.monotonic() + 6.0
                        await asyncio.sleep(1.0)
                        continue

                if at_pw:
                    if now < pw_deadline:
                        await asyncio.sleep(0.4)
                        continue
                    if await _fill_google_password(page, account.secret):
                        _debug("entered password")
                        pw_deadline = time.monotonic() + 8.0
                        await asyncio.sleep(1.0)
                        continue

                if at_email or at_pw:
                    await asyncio.sleep(0.6)
                    continue

            await _click_continue(page)
            await asyncio.sleep(1.0)

        raise RetryableBatcherError(ErrorCode.auth_temporary_failure, "yepapi OAuth flow timed out")

    async def fetch_tokens(self, account: NormalizedAccount, auth_state: dict[str, Any], session: Any) -> dict[str, str]:
        cookies = auth_state.get("cookies", "")
        if not cookies:
            raise NonRetryableBatcherError(ErrorCode.auth_temporary_failure, "no session cookies")

        key_name = account.identifier.split("@")[0].lower()[:20]

        async with aiohttp.ClientSession() as http:
            async with http.post(
                YEPAPI_KEYGEN_URL,
                headers={
                    "cookie": cookies,
                    "content-type": "text/plain;charset=UTF-8",
                    "accept": "text/x-component",
                    "next-action": YEPAPI_KEYGEN_ACTION,
                    "origin": "https://www.yepapi.com",
                    "referer": YEPAPI_KEYGEN_URL,
                    "user-agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
                },
                data=json.dumps([key_name]),
                ssl=_SSL_CTX,
                timeout=aiohttp.ClientTimeout(total=15),
            ) as resp:
                body = await resp.text()
                if resp.status == 200 and "yep_sk_" in body:
                    start = body.index("yep_sk_")
                    end = len(body)
                    for ch in ['"', "'", ",", "}", "\n"]:
                        pos = body.find(ch, start)
                        if 0 < pos < end:
                            end = pos
                    api_key = body[start:end]
                    _debug(f"generated API key: {api_key[:20]}...")
                    return {"api_key": api_key}

                raise RetryableBatcherError(ErrorCode.auth_temporary_failure, f"keygen failed HTTP {resp.status}: {body[:200]}")

    async def fetch_quota(self, account: NormalizedAccount, tokens: dict[str, str], session: Any) -> dict[str, Any] | None:
        return {"total": 5.0, "used": 0.0}

    async def cleanup_session(self, session: Any) -> None:
        if session and not session.get("stub"):
            manager = session.get("manager")
            if manager:
                try:
                    await manager.__aexit__(None, None, None)
                except Exception:
                    pass
