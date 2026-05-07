#!/usr/bin/env python3
"""Playwright: complete the region selection flow on codebuddy registration page."""

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

        # ── Login flow (fast) ──
        print("[1] Login flow...")
        await page.goto(TARGET, wait_until="networkidle", timeout=30000)

        iframe_el = await page.query_selector("iframe")
        if iframe_el:
            iframe_src = await iframe_el.get_attribute("src")
            if iframe_src:
                await page.goto(iframe_src, wait_until="networkidle", timeout=30000)

        login_tab = await page.query_selector("text=Log in")
        if login_tab:
            await login_tab.click()
            await page.wait_for_timeout(1000)

        google_link = await page.query_selector('a[href*="/broker/google/login"]')
        if google_link:
            href = await google_link.get_attribute("href")
            if href and href.startswith("/"):
                href = "https://www.codebuddy.ai" + href
            await page.goto(href, wait_until="networkidle", timeout=30000)

        await page.wait_for_selector('input[type="email"], #identifierId', timeout=15000)
        await page.fill('input[type="email"], #identifierId', EMAIL)
        await page.wait_for_timeout(500)
        next_btn = await page.query_selector("#identifierNext button") or await page.query_selector("#identifierNext")
        if next_btn:
            await next_btn.click()
        else:
            await page.keyboard.press("Enter")
        await page.wait_for_timeout(3000)

        await page.wait_for_selector('input[type="password"]', timeout=15000)
        await page.wait_for_timeout(1000)
        await page.fill('input[type="password"]', PASSWORD)
        await page.wait_for_timeout(500)
        next_btn2 = await page.query_selector("#passwordNext button") or await page.query_selector("#passwordNext")
        if next_btn2:
            await next_btn2.click()
        else:
            await page.keyboard.press("Enter")

        # ── Wait for registration page ──
        print("[2] Waiting for registration page...")
        for i in range(30):
            await page.wait_for_timeout(1000)
            url = page.url
            if "register/user/complete" in url:
                print(f"    ✓ On registration page (tick={i})")
                break
            if "codebuddy.ai" in url and "accounts.google" not in url:
                print(f"    tick={i} url={url[:100]}")
        else:
            print(f"    ✗ Never reached registration page. Final URL: {page.url[:100]}")
            await browser.close()
            return

        # Wait for SPA render
        await page.wait_for_timeout(3000)
        await page.screenshot(path="pw_region_01_page.png")

        # ── STEP 1: Click the Registration location input ──
        print("\n[3] Clicking Registration location input...")
        
        # Try multiple selectors
        input_selectors = [
            'input[placeholder="Registration location"]',
            'input.t-input__inner',
            '.t-input input',
            'input[readonly]',
        ]
        
        clicked = False
        for sel in input_selectors:
            el = await page.query_selector(sel)
            if el:
                visible = await el.is_visible()
                if visible:
                    await el.click()
                    clicked = True
                    print(f"    ✓ Clicked: {sel}")
                    break
        
        if not clicked:
            print("    ✗ No input found!")
            await browser.close()
            return

        await page.wait_for_timeout(2000)
        await page.screenshot(path="pw_region_02_dropdown_open.png")

        # ── STEP 2: Inspect what appeared ──
        print("\n[4] Inspecting dropdown/popup...")
        
        # Check for popup/overlay/dropdown
        popup_info = await page.evaluate("""() => {
            // Check for any new visible elements that appeared
            const selectors = [
                '[role="listbox"]',
                '[role="option"]',
                '.t-select-option',
                '.t-popup',
                '.t-popup__content',
                '.t-select__dropdown',
                '.dropdown-overlay',
                '.t-select-panel',
                '[class*="popup"]',
                '[class*="dropdown"]',
                '[class*="overlay"]',
                '[class*="select"]',
                '[class*="option"]',
                '[class*="menu"]',
                '[class*="list"]',
            ];
            
            const results = [];
            for (const sel of selectors) {
                const els = document.querySelectorAll(sel);
                for (const el of els) {
                    if (el.offsetParent !== null || getComputedStyle(el).display !== 'none') {
                        const rect = el.getBoundingClientRect();
                        if (rect.width > 0 && rect.height > 0) {
                            results.push({
                                selector: sel,
                                tag: el.tagName,
                                class: (el.className || '').toString().substring(0, 80),
                                text: (el.textContent || '').trim().substring(0, 100),
                                childCount: el.children.length,
                                x: Math.round(rect.x),
                                y: Math.round(rect.y),
                                w: Math.round(rect.width),
                                h: Math.round(rect.height),
                            });
                        }
                    }
                }
            }
            return results.slice(0, 30);
        }""")
        
        print(f"    Found {len(popup_info)} popup elements:")
        for info in popup_info:
            print(f"      {info['selector']:30s} | {info['tag']:6s} | {info.get('class','')[:40]:40s} | children={info['childCount']} | text={info['text'][:60]}")

        # Also dump ALL new visible elements
        all_visible = await page.evaluate("""() => {
            const all = document.querySelectorAll('*');
            const results = [];
            for (const el of all) {
                const rect = el.getBoundingClientRect();
                // Only elements that are in the viewport and visible
                if (rect.width < 5 || rect.height < 5) continue;
                if (el.offsetParent === null && getComputedStyle(el).position !== 'fixed') continue;
                const text = (el.textContent || '').trim();
                const cls = (el.className || '').toString();
                // Only interesting elements (with text containing country names or specific classes)
                if (text.includes('Singapore') || text.includes('United States') || 
                    text.includes('Japan') || text.includes('China') ||
                    cls.includes('option') || cls.includes('popup') || 
                    cls.includes('dropdown') || cls.includes('overlay') ||
                    cls.includes('select') || cls.includes('panel') ||
                    cls.includes('menu') || cls.includes('list')) {
                    results.push({
                        tag: el.tagName,
                        class: cls.substring(0, 80),
                        text: text.substring(0, 80),
                        x: Math.round(rect.x),
                        y: Math.round(rect.y),
                        w: Math.round(rect.width),
                        h: Math.round(rect.height),
                    });
                }
            }
            return results.slice(0, 30);
        }""")
        
        print(f"\n    Country/dropdown elements ({len(all_visible)}):")
        for info in all_visible:
            print(f"      {info['tag']:6s} | {info.get('class','')[:50]:50s} | {info['w']}x{info['h']} at ({info['x']},{info['y']}) | text={info['text'][:60]}")

        # ── STEP 3: Try typing Singapore ──
        print("\n[5] Typing 'Singapore' in the input...")
        
        # Check if input is editable or readonly
        input_state = await page.evaluate("""() => {
            const el = document.querySelector('input[placeholder="Registration location"]') || 
                       document.querySelector('input.t-input__inner');
            if (!el) return {found: false};
            return {
                found: true,
                readonly: el.readOnly,
                disabled: el.disabled,
                value: el.value,
                type: el.type,
                tagName: el.tagName,
            };
        }""")
        print(f"    Input state: {input_state}")
        
        if input_state.get('found'):
            if input_state.get('readonly'):
                print("    Input is readonly — trying to clear and type via JS...")
                await page.evaluate("""() => {
                    const el = document.querySelector('input[placeholder="Registration location"]') || 
                               document.querySelector('input.t-input__inner');
                    if (el) {
                        el.readOnly = false;
                        el.value = '';
                        el.dispatchEvent(new Event('input', {bubbles: true}));
                        el.dispatchEvent(new Event('focus', {bubbles: true}));
                    }
                }""")
            
            # Type Singapore
            input_el = await page.query_selector('input[placeholder="Registration location"]') or \
                       await page.query_selector('input.t-input__inner')
            if input_el:
                await input_el.click()
                await page.wait_for_timeout(500)
                await input_el.fill("")
                await page.wait_for_timeout(300)
                await input_el.type("Singapore", delay=50)
                await page.wait_for_timeout(2000)
                await page.screenshot(path="pw_region_03_typed_singapore.png")
                
                # Check what appeared after typing
                after_type = await page.evaluate("""() => {
                    const all = document.querySelectorAll('*');
                    const results = [];
                    for (const el of all) {
                        const rect = el.getBoundingClientRect();
                        if (rect.width < 5 || rect.height < 5) continue;
                        if (el.offsetParent === null && getComputedStyle(el).position !== 'fixed') continue;
                        const text = (el.textContent || '').trim();
                        if (text.toLowerCase().includes('singapore')) {
                            results.push({
                                tag: el.tagName,
                                class: (el.className || '').toString().substring(0, 80),
                                text: text.substring(0, 80),
                                x: Math.round(rect.x),
                                y: Math.round(rect.y),
                                w: Math.round(rect.width),
                                h: Math.round(rect.height),
                                role: el.getAttribute('role'),
                                childCount: el.children.length,
                            });
                        }
                    }
                    return results;
                }""")
                
                print(f"\n    Singapore elements after typing ({len(after_type)}):")
                for info in after_type:
                    print(f"      {info['tag']:6s} | role={info.get('role',''):10s} | {info.get('class','')[:40]:40s} | children={info['childCount']} | {info['w']}x{info['h']} at ({info['x']},{info['y']}) | text={info['text'][:60]}")

                # ── STEP 4: Click Singapore option ──
                if after_type:
                    # Find the most specific (smallest, leaf) element
                    leaf = min(after_type, key=lambda x: x['childCount'])
                    print(f"\n[6] Clicking Singapore: {leaf['tag']} at ({leaf['x']},{leaf['y']})")
                    await page.mouse.click(leaf['x'] + leaf['w']//2, leaf['y'] + leaf['h']//2)
                    await page.wait_for_timeout(2000)
                    await page.screenshot(path="pw_region_04_after_click_singapore.png")
                    
                    # Check input value
                    val = await page.evaluate("""() => {
                        const el = document.querySelector('input[placeholder="Registration location"]') || 
                                   document.querySelector('input.t-input__inner');
                        return el ? el.value : 'NOT FOUND';
                    }""")
                    print(f"    Input value after click: {val}")
                    
                    # ── STEP 5: Look for Submit button ──
                    print("\n[7] Looking for Submit button...")
                    await page.wait_for_timeout(1000)
                    
                    submit_elements = await page.evaluate("""() => {
                        const all = document.querySelectorAll('button, [role="button"], div, span, a, input[type="submit"]');
                        const results = [];
                        for (const el of all) {
                            if (el.offsetParent === null) continue;
                            const txt = (el.textContent || '').trim();
                            if (txt.toLowerCase().includes('submit') || txt.toLowerCase().includes('confirm') || 
                                txt.toLowerCase().includes('continue') || txt.toLowerCase().includes('get started') ||
                                txt.toLowerCase().includes('complete')) {
                                const rect = el.getBoundingClientRect();
                                if (rect.width > 10 && rect.height > 10) {
                                    results.push({
                                        tag: el.tagName,
                                        text: txt.substring(0, 50),
                                        class: (el.className || '').toString().substring(0, 80),
                                        role: el.getAttribute('role'),
                                        x: Math.round(rect.x + rect.width/2),
                                        y: Math.round(rect.y + rect.height/2),
                                        w: Math.round(rect.width),
                                        h: Math.round(rect.height),
                                        childCount: el.children.length,
                                    });
                                }
                            }
                        }
                        return results;
                    }""")
                    
                    print(f"    Submit-like elements ({len(submit_elements)}):")
                    for info in submit_elements:
                        print(f"      {info['tag']:6s} | role={info.get('role',''):10s} | {info.get('class','')[:50]:50s} | children={info['childCount']} | text={info['text']}")
                    
                    # Also dump ALL visible elements to see what's on page
                    all_now = await page.evaluate("""() => {
                        const all = document.querySelectorAll('button, [role="button"], div[class*="cursor"], div[class*="btn"], a[class*="btn"]');
                        const results = [];
                        for (const el of all) {
                            if (el.offsetParent === null) continue;
                            const rect = el.getBoundingClientRect();
                            if (rect.width < 10 || rect.height < 10) continue;
                            const txt = (el.textContent || '').trim();
                            if (txt.length > 0 && txt.length < 50) {
                                results.push({
                                    tag: el.tagName,
                                    text: txt,
                                    class: (el.className || '').toString().substring(0, 80),
                                    x: Math.round(rect.x + rect.width/2),
                                    y: Math.round(rect.y + rect.height/2),
                                    w: Math.round(rect.width),
                                    h: Math.round(rect.height),
                                });
                            }
                        }
                        return results;
                    }""")
                    
                    print(f"\n    All button-like elements ({len(all_now)}):")
                    for info in all_now:
                        print(f"      {info['tag']:6s} | {info.get('class','')[:50]:50s} | at ({info['x']},{info['y']}) {info['w']}x{info['h']} | text={info['text'][:40]}")

                    # ── STEP 6: Click Submit if found ──
                    submit_candidates = [e for e in submit_elements if e['childCount'] <= 2 and e['h'] > 20]
                    if submit_candidates:
                        target = submit_candidates[0]
                        print(f"\n[8] Clicking submit: {target['text']} at ({target['x']},{target['y']})")
                        await page.mouse.click(target['x'], target['y'])
                        await page.wait_for_timeout(5000)
                        await page.screenshot(path="pw_region_05_after_submit.png")
                        print(f"    URL after submit: {page.url[:120]}")
                    else:
                        print("\n    ✗ No submit button found!")
                        
                        # Full page HTML dump
                        html = await page.evaluate("() => document.body.innerHTML.substring(0, 8000)")
                        print(f"\n    === FULL HTML ===\n{html}")

        print("\n[DONE]")
        
        # Final cookies
        cookies = await context.cookies()
        codebuddy_cookies = [c for c in cookies if 'codebuddy' in c.get('domain', '')]
        print(f"\nCodeBuddy cookies: {len(codebuddy_cookies)}")
        for c in codebuddy_cookies:
            print(f"  {c['name']}={c['value'][:20]}...")
        
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
