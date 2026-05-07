#!/usr/bin/env python3
"""Playwright: complete full region selection + submit on codebuddy."""

import asyncio
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

        # ── Login flow ──
        print("[1] Login...")
        await page.goto(TARGET, wait_until="networkidle", timeout=30000)
        iframe_el = await page.query_selector("iframe")
        if iframe_el:
            src = await iframe_el.get_attribute("src")
            if src:
                await page.goto(src, wait_until="networkidle", timeout=30000)

        tab = await page.query_selector("text=Log in")
        if tab:
            await tab.click()
            await page.wait_for_timeout(1000)

        link = await page.query_selector('a[href*="/broker/google/login"]')
        if link:
            href = await link.get_attribute("href")
            if href and href.startswith("/"):
                href = "https://www.codebuddy.ai" + href
            await page.goto(href, wait_until="networkidle", timeout=30000)

        await page.wait_for_selector('input[type="email"], #identifierId', timeout=15000)
        await page.fill('input[type="email"], #identifierId', EMAIL)
        await page.wait_for_timeout(500)
        btn = await page.query_selector("#identifierNext button") or await page.query_selector("#identifierNext")
        if btn: await btn.click()
        else: await page.keyboard.press("Enter")
        await page.wait_for_timeout(3000)

        await page.wait_for_selector('input[type="password"]', timeout=15000)
        await page.wait_for_timeout(1000)
        await page.fill('input[type="password"]', PASSWORD)
        await page.wait_for_timeout(500)
        btn2 = await page.query_selector("#passwordNext button") or await page.query_selector("#passwordNext")
        if btn2: await btn2.click()
        else: await page.keyboard.press("Enter")

        # ── Wait for registration page ──
        print("[2] Waiting for registration page...")
        for i in range(30):
            await page.wait_for_timeout(1000)
            if "register/user/complete" in page.url:
                print(f"    ✓ On registration page (tick={i})")
                break
        else:
            print(f"    ✗ Final URL: {page.url[:100]}")
            await browser.close()
            return

        await page.wait_for_timeout(3000)

        # ── STEP 1: Click input to open dropdown ──
        print("[3] Clicking Registration location input...")
        input_el = await page.query_selector('input[placeholder="Registration location"]')
        if not input_el:
            input_el = await page.query_selector('input.t-input__inner')
        if input_el:
            await input_el.click()
            print("    ✓ Clicked")
        else:
            print("    ✗ Input not found!")
            await browser.close()
            return

        await page.wait_for_timeout(1500)

        # ── STEP 2: Click Singapore from the LI list ──
        print("[4] Clicking Singapore...")
        sg = await page.query_selector('ul.dropdown-section li:has-text("Singapore")')
        if not sg:
            # Fallback
            lis = await page.query_selector_all('.t-popup__content li')
            for li in lis:
                txt = await li.text_content()
                if txt and txt.strip() == "Singapore":
                    sg = li
                    break

        if sg:
            await sg.click()
            print("    ✓ Clicked Singapore")
        else:
            print("    ✗ Singapore LI not found!")
            await browser.close()
            return

        await page.wait_for_timeout(2000)

        # Verify
        val = await page.evaluate("""() => {
            const el = document.querySelector('input[placeholder="Registration location"]');
            return el ? el.value : 'NOT FOUND';
        }""")
        print(f"    Input value: {val}")

        # ── STEP 3: Find and click Submit ──
        print("[5] Looking for Submit button...")
        await page.wait_for_timeout(1000)

        # Dump all button-like elements
        btns = await page.evaluate("""() => {
            const all = document.querySelectorAll('button, [role="button"], div[class*="cursor"], div[class*="btn"]');
            const r = [];
            for (const el of all) {
                if (el.offsetParent === null) continue;
                const txt = (el.textContent || '').trim();
                if (txt.length > 0 && txt.length < 50) {
                    const rect = el.getBoundingClientRect();
                    r.push({tag: el.tagName, text: txt, cls: (el.className||'').toString().substring(0,60), x: Math.round(rect.x+rect.width/2), y: Math.round(rect.y+rect.height/2), w: Math.round(rect.width), h: Math.round(rect.height)});
                }
            }
            return r;
        }""")
        print(f"    Button-like elements ({len(btns)}):")
        for b in btns:
            print(f"      {b['tag']:6s} | {b.get('cls','')[:50]:50s} | {b['w']}x{b['h']} at ({b['x']},{b['y']}) | text={b['text'][:40]}")

        # Click Submit
        submit = None
        for b in btns:
            if b['text'].strip().lower() == 'submit':
                submit = b
                break
        if not submit:
            for b in btns:
                if 'submit' in b['text'].strip().lower():
                    submit = b
                    break

        if submit:
            print(f"\n[6] Clicking Submit at ({submit['x']},{submit['y']})")
            await page.mouse.click(submit['x'], submit['y'])
            await page.wait_for_timeout(5000)
            print(f"    URL after submit: {page.url[:120]}")
            await page.screenshot(path="pw_full_after_submit.png")

            if "register/user/complete" not in page.url:
                print("    ✓ SUCCESS — navigated away from registration!")
            else:
                print("    ✗ Still on registration page")
                # Try JS click
                print("    Trying JS click...")
                await page.evaluate("""() => {
                    const all = document.querySelectorAll('button, [role="button"], div, span');
                    for (const el of all) {
                        if (el.offsetParent === null) continue;
                        const txt = (el.textContent || '').trim();
                        if (/^submit$/i.test(txt)) {
                            el.click();
                            return true;
                        }
                    }
                    return false;
                }""")
                await page.wait_for_timeout(5000)
                print(f"    URL after JS click: {page.url[:120]}")
        else:
            print("    ✗ No Submit button found!")
            html = await page.evaluate("() => document.body.innerHTML.substring(0, 5000)")
            print(f"\n    HTML:\n{html}")

        # Final cookies
        cookies = await context.cookies()
        cb = [c for c in cookies if 'codebuddy' in c.get('domain', '')]
        print(f"\n[DONE] CodeBuddy cookies: {len(cb)}")
        for c in cb[:5]:
            print(f"  {c['name']}={c['value'][:20]}...")

        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
