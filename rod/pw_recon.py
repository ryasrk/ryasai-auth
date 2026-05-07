#!/usr/bin/env python3
"""Playwright recon: login to codebuddy via Google OAuth and inspect the registration page."""

import asyncio
import os
import sys
import time

from playwright.async_api import async_playwright

EMAIL = "lucindaunger@gmilia.com"
PASSWORD = "qwertyui"
TARGET = "https://www.codebuddy.ai/login"


async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        context = await browser.new_context(
            viewport={"width": 1920, "height": 1080},
            user_agent="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
        )
        page = await context.new_page()

        print(f"[1] Navigating to {TARGET}")
        await page.goto(TARGET, wait_until="networkidle", timeout=30000)
        await page.screenshot(path="pw_01_landing.png")
        print(f"    URL: {page.url}")

        # Step 1: Find and enter iframe
        print("[2] Looking for iframe...")
        iframe_el = await page.query_selector("iframe")
        if iframe_el:
            iframe_src = await iframe_el.get_attribute("src")
            print(f"    iframe src: {iframe_src[:100] if iframe_src else 'none'}")
            # Navigate directly to iframe URL
            if iframe_src:
                await page.goto(iframe_src, wait_until="networkidle", timeout=30000)
                await page.screenshot(path="pw_02_iframe_page.png")
                print(f"    URL after iframe nav: {page.url}")
        else:
            print("    No iframe found")

        # Step 2: Click "Log in" tab
        print("[3] Clicking 'Log in' tab...")
        login_tab = await page.query_selector("text=Log in")
        if login_tab:
            await login_tab.click()
            await page.wait_for_timeout(1000)
            await page.screenshot(path="pw_03_login_tab.png")
            print("    Clicked Log in tab")
        else:
            print("    No 'Log in' tab found")

        # Step 3: Click Google login
        print("[4] Looking for Google login link...")
        google_link = await page.query_selector('a[href*="/broker/google/login"]')
        if google_link:
            href = await google_link.get_attribute("href")
            if href and href.startswith("/"):
                href = "https://www.codebuddy.ai" + href
            print(f"    Google link: {href[:80] if href else 'none'}")
            await page.goto(href, wait_until="networkidle", timeout=30000)
            await page.screenshot(path="pw_04_google_login.png")
            print(f"    URL: {page.url}")
        else:
            # Try text-based
            google_btn = await page.query_selector("text=Log in with Google")
            if google_btn:
                await google_btn.click()
                await page.wait_for_timeout(3000)
            else:
                print("    No Google login found!")
                links = await page.eval_on_selector_all("a", "els => els.map(e => ({href: e.href, text: e.textContent.trim().substring(0,50)}))")
                print(f"    Available links: {links[:10]}")
                return

        # Step 4: Google email
        print("[5] Filling email...")
        await page.wait_for_selector('input[type="email"], #identifierId', timeout=15000)
        await page.screenshot(path="pw_05_email_page.png")
        await page.fill('input[type="email"], #identifierId', EMAIL)
        await page.wait_for_timeout(500)

        # Click Next
        next_btn = await page.query_selector("#identifierNext button") or await page.query_selector("#identifierNext")
        if next_btn:
            await next_btn.click()
        else:
            await page.keyboard.press("Enter")
        await page.wait_for_timeout(3000)
        await page.screenshot(path="pw_06_after_email.png")
        print(f"    URL: {page.url}")

        # Check for errors
        error_el = await page.query_selector('[role="alert"], .o6cuMc, .dEOOab')
        if error_el:
            error_text = await error_el.text_content()
            print(f"    ⚠️ Google error: {error_text}")

        # Step 5: Google password
        print("[6] Filling password...")
        try:
            await page.wait_for_selector('input[type="password"]', timeout=15000)
        except Exception as e:
            print(f"    Password field not found: {e}")
            await page.screenshot(path="pw_06b_no_password.png")
            # Check if challenge
            if "/challenge/" in page.url:
                print(f"    Challenge page: {page.url}")
            return

        await page.wait_for_timeout(1000)
        await page.fill('input[type="password"]', PASSWORD)
        await page.wait_for_timeout(500)

        next_btn2 = await page.query_selector("#passwordNext button") or await page.query_selector("#passwordNext")
        if next_btn2:
            await next_btn2.click()
        else:
            await page.keyboard.press("Enter")

        print("[7] Waiting for redirect...")
        # Wait for redirect away from Google
        for i in range(60):
            await page.wait_for_timeout(1000)
            url = page.url
            print(f"    tick={i} url={url[:120]}")

            if "accounts.google.com" in url:
                # Handle consent
                if any(x in url for x in ["/consent", "/oauthchooseaccount", "/speedbump"]):
                    print("    Consent page detected, trying to click Allow...")
                    allow_btn = await page.query_selector("#submit_approve_access") or \
                                await page.query_selector('button[type="submit"]')
                    if allow_btn:
                        await allow_btn.click()
                        await page.wait_for_timeout(2000)
                continue

            if "codebuddy.ai" in url:
                # Skip broker callbacks
                if "/auth/realms/copilot/broker/" in url:
                    continue

                print(f"\n[8] Landed on codebuddy: {url[:150]}")
                await page.screenshot(path="pw_08_codebuddy_landed.png")

                # Wait a bit for any redirects
                await page.wait_for_timeout(3000)
                new_url = page.url
                print(f"    After 3s wait: {new_url[:150]}")
                await page.screenshot(path="pw_08b_after_wait.png")

                # Check if we're on registration page
                if "/register/user/complete" in new_url or "/login/select" in new_url:
                    print("\n[9] REGISTRATION/SELECT PAGE DETECTED!")
                    print(f"    URL: {new_url}")

                    # Wait more for SPA to render
                    await page.wait_for_timeout(5000)
                    final_url = page.url
                    print(f"    After 5s more: {final_url[:150]}")
                    await page.screenshot(path="pw_09_registration.png")

                    # Dump ALL visible elements
                    elements = await page.evaluate("""() => {
                        const els = document.querySelectorAll('div, span, button, input, select, a, [role="combobox"], [role="option"], [role="button"]');
                        const result = [];
                        for (const el of els) {
                            if (el.offsetParent === null) continue;
                            const rect = el.getBoundingClientRect();
                            if (rect.width < 5 || rect.height < 5) continue;
                            const text = (el.textContent || '').trim().substring(0, 80);
                            if (!text && el.tagName !== 'INPUT') continue;
                            result.push({
                                tag: el.tagName,
                                text: text,
                                class: (el.className || '').toString().substring(0, 80),
                                id: el.id,
                                role: el.getAttribute('role'),
                                type: el.getAttribute('type'),
                                placeholder: el.getAttribute('placeholder'),
                                readonly: el.hasAttribute('readonly'),
                                x: Math.round(rect.x),
                                y: Math.round(rect.y),
                                w: Math.round(rect.width),
                                h: Math.round(rect.height),
                            });
                        }
                        return result.slice(0, 80);
                    }""")

                    print(f"\n    === VISIBLE ELEMENTS ({len(elements)}) ===")
                    for el in elements:
                        tag = el.get('tag', '')
                        role = el.get('role') or ''
                        ph = el.get('placeholder') or ''
                        cls = (el.get('class') or '')[:40]
                        txt = (el.get('text') or '')[:50]
                        print(f"    {tag:8s} | {role:10s} | {ph:30s} | {cls:40s} | text={txt}")

                    # Dump full HTML
                    html = await page.evaluate("() => document.body.innerHTML.substring(0, 5000)")
                    print(f"\n    === PAGE HTML (first 5000 chars) ===")
                    print(html)

                    # Try to find the dropdown
                    print("\n[10] Looking for dropdown...")
                    dropdown_input = await page.query_selector('div.t-input input[placeholder="Registration location"]')
                    if dropdown_input:
                        print("    Found 'Registration location' input!")
                        await dropdown_input.click()
                        await page.wait_for_timeout(2000)
                        await page.screenshot(path="pw_10_dropdown_opened.png")

                        # Check overlay
                        overlay = await page.evaluate("""() => {
                            const els = document.querySelectorAll('.dropdown-overlay *, [role="option"], [role="listbox"] *, li');
                            const result = [];
                            for (const el of els) {
                                if (el.offsetParent === null) continue;
                                const text = (el.textContent || '').trim();
                                if (text) result.push({tag: el.tagName, text: text.substring(0, 50), class: (el.className||'').toString().substring(0,40)});
                            }
                            return result.slice(0, 30);
                        }""")
                        print(f"    Dropdown options: {overlay}")
                    else:
                        print("    No 'Registration location' input found")
                        # Try other selectors
                        for sel in ['[role="combobox"]', '.t-select-input', 'input[readonly]', 'select', 'input']:
                            el = await page.query_selector(sel)
                            if el:
                                vis = await el.is_visible()
                                text = await el.evaluate("e => e.value || e.textContent || e.placeholder || ''")
                                print(f"    Found {sel}: visible={vis}, value={text[:50]}")

                    # Look for Submit button
                    print("\n[11] Looking for Submit button...")
                    submit_info = await page.evaluate("""() => {
                        const all = document.querySelectorAll('button, [role="button"], div, span, a');
                        const result = [];
                        for (const el of all) {
                            if (el.offsetParent === null) continue;
                            const txt = (el.textContent || '').trim();
                            if (txt.toLowerCase().includes('submit') && txt.length < 40) {
                                const rect = el.getBoundingClientRect();
                                result.push({
                                    tag: el.tagName,
                                    text: txt,
                                    class: (el.className || '').toString().substring(0, 60),
                                    x: Math.round(rect.x + rect.width/2),
                                    y: Math.round(rect.y + rect.height/2),
                                    w: Math.round(rect.width),
                                    h: Math.round(rect.height),
                                });
                            }
                        }
                        return result;
                    }""")
                    print(f"    Submit elements: {submit_info}")

                else:
                    print(f"    Not a registration page, extracting cookies...")

                break

        print("\n[DONE]")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
