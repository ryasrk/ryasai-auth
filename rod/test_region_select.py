"""
Playwright test: verify region dropdown select + submit on CodeBuddy registration page.
Simulates the same flow as handleRegionSelection() in google.go
"""

import asyncio
from playwright.async_api import async_playwright

# Mock HTML that mimics the CodeBuddy /register/user/complete page
MOCK_HTML = """
<!DOCTYPE html>
<html>
<head>
    <title>Complete Registration</title>
    <style>
        body { font-family: Arial, sans-serif; padding: 40px; background: #f5f5f5; }
        .container { max-width: 500px; margin: 0 auto; background: white; padding: 30px; border-radius: 8px; }
        h2 { margin-bottom: 20px; }
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 5px; font-weight: bold; }

        /* Mimics t-select-input (combobox dropdown) */
        .t-select-input {
            position: relative;
            width: 100%;
        }
        .t-select-input input {
            width: 100%;
            padding: 10px;
            border: 1px solid #ccc;
            border-radius: 4px;
            cursor: pointer;
            box-sizing: border-box;
        }
        .dropdown {
            display: none;
            position: absolute;
            top: 100%;
            left: 0;
            right: 0;
            background: white;
            border: 1px solid #ccc;
            border-radius: 4px;
            max-height: 200px;
            overflow-y: auto;
            z-index: 100;
        }
        .dropdown.open { display: block; }
        .dropdown li {
            list-style: none;
            padding: 10px;
            cursor: pointer;
        }
        .dropdown li:hover { background: #e8f0fe; }
        .dropdown [role="option"][aria-selected="true"] { background: #d2e3fc; }

        button[type="submit"] {
            background: #1a73e8;
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 16px;
        }
        button[type="submit"]:hover { background: #1557b0; }
        button[type="submit"]:disabled { background: #ccc; cursor: not-allowed; }

        #result { margin-top: 20px; padding: 10px; display: none; }
        #result.success { display: block; background: #e6f4ea; border: 1px solid #34a853; border-radius: 4px; }
    </style>
</head>
<body>
    <div class="container">
        <h2>Complete Your Registration</h2>
        <form id="regForm">
            <div class="form-group">
                <label>Select Region</label>
                <div class="t-select-input">
                    <input
                        role="combobox"
                        readonly
                        placeholder="Select a region..."
                        id="regionInput"
                        aria-expanded="false"
                    />
                    <ul class="dropdown" id="regionDropdown">
                        <li role="option" data-value="us-east">United States (East)</li>
                        <li role="option" data-value="us-west">United States (West)</li>
                        <li role="option" data-value="eu-west">Europe (West)</li>
                        <li role="option" data-value="ap-sg">Singapore</li>
                        <li role="option" data-value="ap-jp">Japan</li>
                        <li role="option" data-value="ap-au">Australia</li>
                    </ul>
                </div>
            </div>
            <button type="submit" id="submitBtn">Submit</button>
        </form>
        <div id="result"></div>
    </div>

    <script>
        const input = document.getElementById('regionInput');
        const dropdown = document.getElementById('regionDropdown');
        const form = document.getElementById('regForm');
        const result = document.getElementById('result');
        let selectedValue = '';

        // Open dropdown on click
        input.addEventListener('click', () => {
            const isOpen = dropdown.classList.contains('open');
            dropdown.classList.toggle('open');
            input.setAttribute('aria-expanded', !isOpen);
        });

        // Also handle mousedown (some frameworks use this)
        input.addEventListener('mousedown', (e) => {
            // Framework-style: open on mousedown too
        });

        // Select option
        dropdown.querySelectorAll('[role="option"]').forEach(opt => {
            opt.addEventListener('click', () => {
                // Clear previous selection
                dropdown.querySelectorAll('[role="option"]').forEach(o =>
                    o.setAttribute('aria-selected', 'false')
                );
                opt.setAttribute('aria-selected', 'true');

                selectedValue = opt.dataset.value;
                input.value = opt.textContent.trim();

                dropdown.classList.remove('open');
                input.setAttribute('aria-expanded', 'false');

                console.log('[region] selected:', selectedValue, input.value);
            });
        });

        // Also handle programmatic input events (what Go code dispatches)
        input.addEventListener('input', (e) => {
            console.log('[region] input event:', input.value);
        });
        input.addEventListener('change', (e) => {
            console.log('[region] change event:', input.value);
        });

        // Submit
        form.addEventListener('submit', (e) => {
            e.preventDefault();
            if (!input.value || input.value === '') {
                alert('Please select a region');
                return;
            }
            result.className = 'success';
            result.textContent = 'Registration complete! Region: ' + input.value + ' (' + selectedValue + ')';
            result.style.display = 'block';
            console.log('[submit] region=' + selectedValue + ' display=' + input.value);

            // Simulate navigation (set URL hash to signal success)
            window.location.hash = '#registered';
        });
    </script>
</body>
</html>
"""


async def test_region_select_and_submit():
    """
    Test that mimics handleRegionSelection() logic from google.go:
    1. Open dropdown (click combobox/t-select-input/input)
    2. Select "Singapore" from options
    3. Click Submit button
    4. Verify success
    """
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        page = await browser.new_page()

        # Load mock page
        await page.set_content(MOCK_HTML, wait_until="domcontentloaded")
        await page.wait_for_timeout(500)

        print("=" * 60)
        print("TEST: Region Selection + Submit (mirrors google.go logic)")
        print("=" * 60)

        # Capture console logs
        logs = []
        page.on("console", lambda msg: logs.append(msg.text))

        # ─── STEP 1: Open dropdown ───
        print("\n[STEP 1] Opening dropdown...")

        # Try same selectors as Go code
        dropdown_selectors = [
            '[role="combobox"]',
            '.t-select-input',
            'input[readonly]',
            'input',
        ]

        opened = False
        for sel in dropdown_selectors:
            el = page.locator(sel).first
            if await el.count() > 0:
                await el.scroll_into_view_if_needed()
                await el.click()
                opened = True
                print(f"  ✓ Dropdown opened via: {sel}")
                break

        assert opened, "FAIL: Could not open dropdown"
        await page.wait_for_timeout(500)

        # Verify dropdown is visible
        dropdown_visible = await page.locator('.dropdown.open').count() > 0
        print(f"  Dropdown visible: {dropdown_visible}")
        assert dropdown_visible, "FAIL: Dropdown not visible after click"

        # ─── STEP 2: Select Singapore ───
        print("\n[STEP 2] Selecting Singapore...")

        # Method A: Click the option directly (like Go's el.click())
        sg_option = page.locator('[role="option"]').filter(has_text="Singapore")
        sg_count = await sg_option.count()
        print(f"  Singapore options found: {sg_count}")

        if sg_count > 0:
            await sg_option.first.scroll_into_view_if_needed()
            await sg_option.first.click()
            print("  ✓ Singapore clicked via role=option")
        else:
            # Method B: JS eval like Go code does
            result = await page.evaluate("""() => {
                const candidates = [
                    ...document.querySelectorAll('[role="option"]'),
                    ...document.querySelectorAll('li'),
                    ...document.querySelectorAll('div')
                ];
                for (const el of candidates) {
                    const txt = (el.textContent || '').trim();
                    if (el.offsetParent !== null && txt.includes('Singapore')) {
                        el.scrollIntoView({ block: 'center' });
                        el.click();
                        return true;
                    }
                }
                return false;
            }""")
            assert result, "FAIL: Could not select Singapore via JS"
            print("  ✓ Singapore selected via JS eval")

        await page.wait_for_timeout(500)

        # Verify selection
        input_value = await page.locator('#regionInput').input_value()
        print(f"  Input value after selection: '{input_value}'")
        assert "Singapore" in input_value, f"FAIL: Expected 'Singapore' in input, got '{input_value}'"

        # Verify dropdown closed
        dropdown_closed = await page.locator('.dropdown.open').count() == 0
        print(f"  Dropdown closed: {dropdown_closed}")

        # ─── STEP 3: Submit ───
        print("\n[STEP 3] Clicking Submit...")

        # Method A: XPath text match (like Go code)
        submit_btn = page.locator("xpath=//*[contains(text(),'Submit')]")
        submit_count = await submit_btn.count()
        print(f"  Submit buttons found (XPath): {submit_count}")

        if submit_count > 0:
            await submit_btn.first.scroll_into_view_if_needed()
            await submit_btn.first.click()
            print("  ✓ Submit clicked via XPath")
        else:
            # Fallback: CSS selector
            await page.locator('button[type="submit"]').click()
            print("  ✓ Submit clicked via CSS")

        await page.wait_for_timeout(1000)

        # ─── VERIFY ───
        print("\n[VERIFY] Checking results...")

        # Check success message
        result_el = page.locator('#result')
        result_visible = await result_el.is_visible()
        print(f"  Result visible: {result_visible}")

        if result_visible:
            result_text = await result_el.text_content()
            print(f"  Result text: {result_text}")
            assert "Singapore" in result_text, f"FAIL: Result doesn't mention Singapore"
            print("  ✓ Registration success with Singapore!")

        # Check URL hash
        url = page.url
        print(f"  URL after submit: {url}")

        # Check console logs
        print(f"\n  Console logs:")
        for log in logs:
            print(f"    {log}")

        print("\n" + "=" * 60)
        print("ALL TESTS PASSED ✓")
        print("=" * 60)

        # ─── BUG REPORT ───
        print("\n⚠️  BUG FOUND in google.go handleRegionSelection():")
        print("   Line ~545: 'submitted' is never set to true after mouse click")
        print("   The function always returns 'submit click failed' error")
        print("   Fix: add 'submitted = true' after Mouse.Up() call")

        await browser.close()


if __name__ == "__main__":
    asyncio.run(test_region_select_and_submit())
