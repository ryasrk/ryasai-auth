package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

func googleOAuthLogin(browser *rod.Browser, req LoginRequest) LoginResult {
	page, err := browser.Page(proto.TargetCreateTarget{URL: req.TargetURL})
	if err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "page open failed: " + err.Error()}
	}
	defer page.Close()

	applyStealthViaAddScript(page)
	applyStealth(page)

	cfg := DefaultStealthConfig()
	if err := ApplyFullStealthToPage(browser, page, cfg); err != nil {
		_ = err
	}

	if req.Provider == "codebuddy" {
		if err := handleCodebuddyLanding(page); err != nil {
			return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "codebuddy landing: " + err.Error()}
		}
	}

	if err := waitForGoogleLogin(page); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	if err := fillEmail(page, req.Email); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "email step: " + err.Error()}
	}

	if err := waitForPasswordStep(page); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	if err := fillPassword(page, req.Password); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "password step: " + err.Error()}
	}

	cookies, err := waitForProviderRedirect(page, req)
	if err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	return LoginResult{Status: "success", Cookies: cookies, Error: ""}
}

func waitForGoogleLogin(page *rod.Page) error {
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		info, err := page.Info()
		if err != nil {
			continue
		}
		if strings.Contains(info.URL, "accounts.google.com") {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for google login page")
}

func fillEmail(page *rod.Page, email string) error {
	el, err := page.Timeout(15 * time.Second).Element(`input[type="email"], input[name="identifier"], #identifierId`)
	if err != nil {
		return fmt.Errorf("email input not found: %w", err)
	}

	scrollNoise(page)
	randomDelay(200, 600)
	humanType(page, el, email)
	randomDelay(300, 800)

	if err := clickNextButton(page); err != nil {
		_ = page.Keyboard.Type(input.Enter)
	}

	return nil
}

func waitForPasswordStep(page *rod.Page) error {
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)

		info, infoErr := page.Info()
		if infoErr != nil {
			continue
		}
		url := info.URL
		if strings.Contains(url, "/challenge/") && !strings.Contains(url, "/challenge/pwd") {
			return fmt.Errorf("google challenge detected (captcha/2fa): %s", url)
		}

		if res, err := page.Eval(`() => {
			const errs = document.querySelectorAll('[class*="error"], [class*="Error"], [role="alert"], .o6cuMc, .dEOOab');
			for (const el of errs) {
				if (el.offsetParent !== null && el.textContent.trim().length > 5) {
					return el.textContent.trim().substring(0, 150);
				}
			}
			return '';
		}`); err == nil && res != nil && res.Value.Str() != "" {
			return fmt.Errorf("google error after email: %s", res.Value.Str())
		}

		if res, err := page.Eval(`() => {
			const el = document.querySelector('input[type="password"], input[name="Passwd"]');
			return el && el.offsetParent !== null;
		}`); err == nil && res != nil && res.Value.Bool() {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for password step")
}

func fillPassword(page *rod.Page, password string) error {
	el, err := page.Timeout(15 * time.Second).Element(`input[type="password"], input[name="Passwd"]`)
	if err != nil {
		return fmt.Errorf("password input not found: %w", err)
	}

	_, err = el.WaitInteractable()
	if err != nil {
		time.Sleep(3 * time.Second)
		_, err = el.WaitInteractable()
		if err != nil {
			pageURL := ""
			if info, e := page.Info(); e == nil {
				pageURL = info.URL
			}
			errText := ""
			if res, e := page.Eval(`() => {
				const errs = document.querySelectorAll('[class*="error"], [class*="Error"], [role="alert"], .o6cuMc');
				const texts = [];
				errs.forEach(el => { if (el.offsetParent !== null) texts.push(el.textContent.trim()); });
				return texts.join(' | ').substring(0, 200);
			}`); e == nil && res != nil {
				errText = res.Value.Str()
			}
			if errText != "" {
				return fmt.Errorf("password input not interactable (page error: %s) url: %s", errText, pageURL)
			}
			return fmt.Errorf("password input not interactable: %w (url: %s)", err, pageURL)
		}
	}

	scrollNoise(page)
	randomDelay(200, 500)
	humanType(page, el, password)
	randomDelay(400, 900)

	if err := clickNextButton(page); err != nil {
		_ = page.Keyboard.Type(input.Enter)
	}

	return nil
}

func clickNextButton(page *rod.Page) error {
	selectors := []string{
		"#identifierNext button",
		"#passwordNext button",
		`div[id="identifierNext"]`,
		`div[id="passwordNext"]`,
		`button[type="submit"]`,
	}

	for _, sel := range selectors {
		has, el, _ := page.Has(sel)
		if has && el != nil {
			randomDelay(100, 300)
			return humanClick(page, el)
		}
	}

	return fmt.Errorf("no next button found")
}

func handleConsentScreen(page *rod.Page) {
	randomDelay(500, 1500)

	selectors := []string{
		`#submit_approve_access`,
		`button[id="submit_approve_access"]`,
		`input[type="submit"]`,
		`#confirm`,
		`button[type="submit"]`,
		`[data-idom-class*="submit"]`,
	}

	for _, sel := range selectors {
		has, el, _ := page.Has(sel)
		if has && el != nil {
			humanClick(page, el)
			randomDelay(800, 1500)
			return
		}
	}

	buttons, _ := page.Elements(`button, input[type="submit"], div[role="button"], a[role="button"], span[role="button"]`)
	for _, btn := range buttons {
		text, _ := btn.Text()
		lower := strings.ToLower(text)
		if strings.Contains(lower, "allow") ||
			strings.Contains(lower, "continue") ||
			strings.Contains(lower, "accept") ||
			strings.Contains(lower, "agree") ||
			strings.Contains(lower, "i agree") ||
			strings.Contains(lower, "next") ||
			strings.Contains(lower, "confirm") ||
			strings.Contains(lower, "proceed") {
			humanClick(page, btn)
			fmt.Fprintf(os.Stderr, "[consent] clicked: %s\n", lower[:minInt(40, len(lower))])
			randomDelay(800, 1500)
			return
		}
	}

	allBtns, _ := page.Elements(`button`)
	for _, btn := range allBtns {
		visible, _ := btn.Visible()
		if visible {
			humanClick(page, btn)
			fmt.Fprintf(os.Stderr, "[consent] clicked first visible button\n")
			randomDelay(800, 1500)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "[consent] no clickable button found\n")
}

func handleRegionSelection(page *rod.Page) error {
	info, err := page.Info()
	if err != nil {
		return err
	}
	if !strings.Contains(info.URL, "/register/user/complete") {
		return nil
	}

	fmt.Fprintf(os.Stderr, "[region] completion page detected\n")
	time.Sleep(3 * time.Second)

	// ── STEP 1: Click the Registration location input to open t-popup dropdown ──
	// The input is READONLY — clicking it opens a t-popup with country options.
	// DO NOT type into it — that breaks the popup state.
	opened := false

	inputSelectors := []string{
		`input[placeholder="Registration location"]`,
		`input.t-input__inner`,
		`.t-input input`,
	}

	for _, sel := range inputSelectors {
		el, err := page.Timeout(8 * time.Second).Element(sel)
		if err != nil {
			continue
		}
		visible, _ := el.Visible()
		if !visible {
			continue
		}

		_ = el.ScrollIntoView()
		time.Sleep(500 * time.Millisecond)

		box, shapeErr := el.Shape()
		if shapeErr != nil {
			continue
		}
		b := box.Box()
		x := b.X + b.Width/2
		y := b.Y + b.Height/2

		_ = page.Mouse.MustMoveTo(x, y)
		time.Sleep(100 * time.Millisecond)
		_ = page.Mouse.Down(proto.InputMouseButtonLeft, 1)
		time.Sleep(50 * time.Millisecond)
		_ = page.Mouse.Up(proto.InputMouseButtonLeft, 1)

		opened = true
		fmt.Fprintf(os.Stderr, "[region] clicked input (%s) at %.0f,%.0f\n", sel, x, y)
		break
	}

	if !opened {
		return fmt.Errorf("could not find/click Registration location input")
	}

	// Wait for t-popup to appear
	time.Sleep(1500 * time.Millisecond)

	// ── STEP 2: Find and click Singapore from the popup's <LI> list ──
	// The popup structure is:
	//   div.t-popup > div.t-popup__content >
	//     div.dropdown-search (search input)
	//     div (Current Region header + current selection)
	//     ul.dropdown-section > li.cursor-pointer (country options)
	//
	// Singapore is an <LI> inside ul.dropdown-section
	selected := false

	sgResult, err := page.Eval(`() => {
		// Look in the popup's option list — LI elements inside ul.dropdown-section
		const items = document.querySelectorAll('ul.dropdown-section li, .t-popup__content li, .t-popup li');
		for (const el of items) {
			if (el.offsetParent === null) continue;
			const txt = (el.textContent || '').trim();
			if (txt === 'Singapore') {
				const rect = el.getBoundingClientRect();
				if (rect.width > 10 && rect.height > 10) {
					return JSON.stringify({
						found: true,
						x: rect.x + rect.width / 2,
						y: rect.y + rect.height / 2,
						text: txt,
					});
				}
			}
		}
		// Fallback: any visible element with exact text "Singapore" (not "Current Region Singapore")
		const all = document.querySelectorAll('.t-popup__content *');
		for (const el of all) {
			if (el.offsetParent === null) continue;
			const txt = (el.textContent || '').trim();
			if (txt === 'Singapore' && el.children.length === 0) {
				const rect = el.getBoundingClientRect();
				if (rect.width > 10 && rect.height > 10) {
					return JSON.stringify({
						found: true,
						x: rect.x + rect.width / 2,
						y: rect.y + rect.height / 2,
						text: txt,
					});
				}
			}
		}
		// Debug: dump what's in the popup
		const debug = [];
		document.querySelectorAll('.t-popup__content *, ul.dropdown-section *').forEach(el => {
			if (el.offsetParent !== null) {
				debug.push(el.tagName + ':' + (el.textContent||'').trim().substring(0,40));
			}
		});
		return JSON.stringify({ found: false, debug: debug.slice(0, 20) });
	}`)

	if err == nil && sgResult != nil {
		raw := sgResult.Value.Str()
		fmt.Fprintf(os.Stderr, "[region] singapore search: %s\n", raw[:minInt(300, len(raw))])

		var parsed map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(raw), &parsed); jsonErr == nil {
			if found, ok := parsed["found"].(bool); ok && found {
				x := parsed["x"].(float64)
				y := parsed["y"].(float64)
				fmt.Fprintf(os.Stderr, "[region] clicking Singapore at %.0f,%.0f\n", x, y)

				_ = page.Mouse.MustMoveTo(x, y)
				time.Sleep(100 * time.Millisecond)
				_ = page.Mouse.Down(proto.InputMouseButtonLeft, 1)
				time.Sleep(50 * time.Millisecond)
				_ = page.Mouse.Up(proto.InputMouseButtonLeft, 1)
				selected = true
			}
		}
	}

	// Fallback: JS click on the LI directly
	if !selected {
		fmt.Fprintf(os.Stderr, "[region] mouse click failed, trying JS click on LI\n")
		jsClick, err := page.Eval(`() => {
			const items = document.querySelectorAll('ul.dropdown-section li, .t-popup__content li');
			for (const el of items) {
				if (el.offsetParent === null) continue;
				const txt = (el.textContent || '').trim();
				if (txt === 'Singapore') {
					el.scrollIntoView({block: 'center'});
					el.click();
					return 'clicked';
				}
			}
			return '';
		}`)
		if err == nil && jsClick != nil && jsClick.Value.Str() == "clicked" {
			selected = true
			fmt.Fprintf(os.Stderr, "[region] JS click on Singapore LI succeeded\n")
		}
	}

	if !selected {
		return fmt.Errorf("Singapore option not found in dropdown popup")
	}

	// Wait for selection to register and popup to close
	time.Sleep(2 * time.Second)

	// ── STEP 3: Verify Singapore is selected ──
	fmt.Fprintf(os.Stderr, "[region] verifying selection...\n")
	verified := false
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		res, err := page.Eval(`() => {
			const input = document.querySelector('input[placeholder="Registration location"]') ||
			              document.querySelector('input.t-input__inner');
			const val = (input?.value || '').trim().toLowerCase();
			const body = (document.body?.innerText || '').toLowerCase();
			return val === 'singapore' || body.includes('current region singapore');
		}`)
		if err == nil && res != nil && res.Value.Bool() {
			verified = true
			fmt.Fprintf(os.Stderr, "[region] ✓ Singapore confirmed\n")
			break
		}
	}
	if !verified {
		fmt.Fprintf(os.Stderr, "[region] WARNING: could not verify Singapore selection, continuing anyway\n")
	}

	time.Sleep(1 * time.Second)

	// ── STEP 4: Click Submit button ──
	// Submit button appears AFTER region is selected.
	// It's typically a button or div with text "Submit".
	submitted := false

	// Strategy 1: find via JS and click with real mouse
	submitResult, err := page.Eval(`() => {
		const all = document.querySelectorAll('button, [role="button"], div, span, a, input[type="submit"]');
		for (const el of all) {
			if (el.offsetParent === null) continue;
			const txt = (el.textContent || '').trim();
			if (/^submit$/i.test(txt)) {
				const rect = el.getBoundingClientRect();
				if (rect.width > 10 && rect.height > 10) {
					return JSON.stringify({
						found: true,
						x: rect.x + rect.width / 2,
						y: rect.y + rect.height / 2,
						tag: el.tagName,
						cls: (el.className || '').toString().substring(0, 60),
					});
				}
			}
		}
		// Looser: contains "submit"
		for (const el of all) {
			if (el.offsetParent === null) continue;
			const txt = (el.textContent || '').trim();
			if (/submit/i.test(txt) && txt.length < 40) {
				const rect = el.getBoundingClientRect();
				if (rect.width > 10 && rect.height > 10) {
					return JSON.stringify({
						found: true,
						x: rect.x + rect.width / 2,
						y: rect.y + rect.height / 2,
						tag: el.tagName,
						cls: (el.className || '').toString().substring(0, 60),
					});
				}
			}
		}
		// Debug: dump visible button-like elements
		const btns = [];
		document.querySelectorAll('button, [role="button"], div[class*="cursor"], div[class*="btn"]').forEach(el => {
			if (el.offsetParent === null) return;
			const txt = (el.textContent || '').trim();
			if (txt.length > 0 && txt.length < 40) {
				btns.push(el.tagName + ':' + txt);
			}
		});
		return JSON.stringify({ found: false, buttons: btns.slice(0, 15) });
	}`)

	if err == nil && submitResult != nil {
		raw := submitResult.Value.Str()
		fmt.Fprintf(os.Stderr, "[region] submit search: %s\n", raw[:minInt(300, len(raw))])

		var parsed map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(raw), &parsed); jsonErr == nil {
			if found, ok := parsed["found"].(bool); ok && found {
				x := parsed["x"].(float64)
				y := parsed["y"].(float64)
				fmt.Fprintf(os.Stderr, "[region] clicking submit at %.0f,%.0f\n", x, y)
				_ = page.Mouse.MustMoveTo(x, y)
				time.Sleep(100 * time.Millisecond)
				_ = page.Mouse.Down(proto.InputMouseButtonLeft, 1)
				time.Sleep(120 * time.Millisecond)
				_ = page.Mouse.Up(proto.InputMouseButtonLeft, 1)
				submitted = true
			}
		}
	}

	// Strategy 2: JS el.click() fallback
	if !submitted {
		fmt.Fprintf(os.Stderr, "[region] mouse submit failed, trying JS click\n")
		jsClick, err := page.Eval(`() => {
			const all = document.querySelectorAll('button, [role="button"], div, span, a');
			for (const el of all) {
				if (el.offsetParent === null) continue;
				const txt = (el.textContent || '').trim();
				if (/^submit$/i.test(txt) || (/submit/i.test(txt) && txt.length < 40)) {
					el.scrollIntoView({block: 'center'});
					el.click();
					el.dispatchEvent(new MouseEvent('click', {bubbles: true}));
					return 'clicked:' + el.tagName + ':' + txt.substring(0, 30);
				}
			}
			return '';
		}`)
		if err == nil && jsClick != nil && jsClick.Value.Str() != "" {
			fmt.Fprintf(os.Stderr, "[region] JS submit click: %s\n", jsClick.Value.Str())
			submitted = true
		}
	}

	// Strategy 3: XPath fallback
	if !submitted {
		xpaths := []string{
			`//*[text()='Submit']`,
			`//*[contains(text(),'Submit')]`,
			`//html/body/div/div/div[3]/div/div/div[2]/div[2]`,
		}
		for _, xpath := range xpaths {
			el, err := page.ElementX(xpath)
			if err != nil || el == nil {
				continue
			}
			visible, _ := el.Visible()
			if !visible {
				continue
			}
			box, shapeErr := el.Shape()
			if shapeErr != nil {
				continue
			}
			b := box.Box()
			_ = page.Mouse.MustMoveTo(b.X+b.Width/2, b.Y+b.Height/2)
			time.Sleep(100 * time.Millisecond)
			_ = page.Mouse.Down(proto.InputMouseButtonLeft, 1)
			time.Sleep(120 * time.Millisecond)
			_ = page.Mouse.Up(proto.InputMouseButtonLeft, 1)
			submitted = true
			fmt.Fprintf(os.Stderr, "[region] submit via XPath: %s\n", xpath)
			break
		}
	}

	if !submitted {
		htmlDump, _ := page.Eval(`() => document.body.innerHTML.substring(0, 3000)`)
		if htmlDump != nil {
			fmt.Fprintf(os.Stderr, "[region] page HTML:\n%s\n", htmlDump.Value.Str())
		}
		return fmt.Errorf("submit button not found")
	}

	fmt.Fprintf(os.Stderr, "[region] submit clicked, waiting for navigation\n")

	// ── STEP 5: Wait for URL to leave /register/user/complete ──
	for i := 0; i < 20; i++ {
		time.Sleep(1 * time.Second)
		info, err := page.Info()
		if err != nil {
			continue
		}
		if !strings.Contains(info.URL, "/register/user/complete") {
			fmt.Fprintf(os.Stderr, "[region] ✓ navigated to: %s\n", info.URL[:minInt(120, len(info.URL))])
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "[region] WARNING: still on registration page after submit\n")
	return nil
}

func waitForProviderRedirect(page *rod.Page, req LoginRequest) (map[string]string, error) {
	providerHost := extractHost(req.TargetURL)
	regionHandled := false

	for i := 0; i < 90; i++ {
		time.Sleep(1 * time.Second)

		info, err := page.Info()
		if err != nil {
			continue
		}
		url := info.URL

		// ← ADD THIS
		fmt.Fprintf(os.Stderr, "[redirect] tick=%d url=%s\n", i, url[:minInt(120, len(url))])

		if strings.Contains(url, "accounts.google.com") {
			if strings.Contains(url, "/speedbump/") ||
				strings.Contains(url, "/consent") ||
				strings.Contains(url, "/oauthchooseaccount") ||
				strings.Contains(url, "/gaplustos") {
				handleConsentScreen(page)
			}
			continue
		}

		if providerHost != "" && strings.Contains(url, providerHost) {
			if req.Provider == "codebuddy" &&
				strings.Contains(url, "/auth/realms/copilot/broker/") {
				continue
			}

			// Region page — either already on it, or on /login/select that will redirect to it
			if req.Provider == "codebuddy" {
				isRegionPage := strings.Contains(url, "/register/user/complete")
				isPreRegionRedirect := strings.Contains(url, "/login/select") &&
					strings.Contains(url, "register%2Fuser%2Fcomplete")

				if isRegionPage || isPreRegionRedirect {
					if isPreRegionRedirect && !isRegionPage {
						// We're on /login/select with redirect_uri pointing to registration
						// Wait for the actual navigation to happen
						fmt.Fprintf(os.Stderr, "[redirect] on /login/select, waiting for redirect to registration page...\n")
						time.Sleep(3 * time.Second)
						// Re-check URL after wait
						if newInfo, e := page.Info(); e == nil {
							newURL := newInfo.URL
							fmt.Fprintf(os.Stderr, "[redirect] after wait url=%s\n", newURL[:minInt(120, len(newURL))])
							if strings.Contains(newURL, "/register/user/complete") {
								isRegionPage = true
							}
						}
					}
					if isRegionPage && !regionHandled {
						regionHandled = true
						fmt.Fprintf(os.Stderr, "[redirect] entering region selection handler\n")
						_ = handleRegionSelection(page)
					}
					continue // keep looping until URL leaves registration
				}
			}

			fmt.Fprintf(os.Stderr, "[redirect] extracting cookies at url=%s\n", url[:minInt(120, len(url))])

			wait := page.WaitRequestIdle(2*time.Second, nil, nil, nil)
			wait()
			return extractCookies(page, providerHost)
		}
	}

	return nil, fmt.Errorf("timeout waiting for redirect to %s", providerHost)
}

func extractCookies(page *rod.Page, host string) (map[string]string, error) {
	_ = page.Timeout(15 * time.Second).WaitLoad()
	time.Sleep(3 * time.Second)

	cookies, err := proto.StorageGetCookies{
		BrowserContextID: "",
	}.Call(page)

	if err != nil {
		pageCookies, err2 := page.Cookies(nil)
		if err2 != nil {
			return nil, fmt.Errorf("failed to get cookies: %w (cdp: %w)", err2, err)
		}
		result := make(map[string]string)
		for _, c := range pageCookies {
			result[c.Name] = c.Value
		}
		return result, nil
	}

	result := make(map[string]string)
	for _, c := range cookies.Cookies {
		if strings.HasSuffix(c.Domain, host) || isTokenCookie(c.Name) {
			result[c.Name] = c.Value
		}
	}

	if len(result) == 0 {
		for _, c := range cookies.Cookies {
			result[c.Name] = c.Value
		}
	}

	if len(result) == 0 {
		return result, fmt.Errorf("no cookies found after redirect to %s", host)
	}

	domains := make(map[string]int)
	for _, c := range cookies.Cookies {
		domains[c.Domain]++
	}
	fmt.Fprintf(os.Stderr, "[cookies] total=%d, provider=%d (host=%s), domains: ", len(cookies.Cookies), len(result), host)
	for d, n := range domains {
		fmt.Fprintf(os.Stderr, "%s(%d) ", d, n)
	}
	fmt.Fprintf(os.Stderr, "\n")

	return result, nil
}

func isTokenCookie(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "session") ||
		strings.Contains(lower, "auth") ||
		strings.Contains(lower, "jwt")
}

func extractHost(rawURL string) string {
	s := rawURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.IndexAny(s, "/?#"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func handleCodebuddyLanding(page *rod.Page) error {
	time.Sleep(2 * time.Second)

	iframeSrc := ""
	for attempt := 0; attempt < 10; attempt++ {
		result, err := page.Eval(`() => {
			const iframe = document.querySelector('iframe');
			if (iframe && iframe.src) return iframe.src;
			return '';
		}`)
		if err == nil && result != nil {
			iframeSrc = result.Value.Str()
		}
		if iframeSrc != "" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if iframeSrc != "" {
		fmt.Fprintf(os.Stderr, "[codebuddy] navigating to iframe URL: %s\n", iframeSrc[:minInt(80, len(iframeSrc))])
		err := page.Navigate(iframeSrc)
		if err != nil {
			return fmt.Errorf("failed to navigate to iframe URL: %w", err)
		}
		_ = page.Timeout(10 * time.Second).WaitLoad()
		time.Sleep(2 * time.Second)
	} else {
		fmt.Fprintf(os.Stderr, "[codebuddy] no iframe found, trying on current page\n")
	}

	tabClicked := false
	for attempt := 0; attempt < 5; attempt++ {
		result, err := page.Eval(`() => {
			const els = document.querySelectorAll('div, span, button, a');
			for (const el of els) {
				const txt = (el.textContent || '').trim();
				if (txt === 'Log in' && el.children.length === 0 && el.offsetParent !== null) {
					el.dispatchEvent(new MouseEvent('mousedown', {bubbles:true}));
					el.dispatchEvent(new MouseEvent('mouseup', {bubbles:true}));
					el.click();
					return true;
				}
			}
			return false;
		}`)
		if err == nil && result != nil && result.Value.Bool() {
			tabClicked = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if tabClicked {
		fmt.Fprintf(os.Stderr, "[codebuddy] clicked Log in tab\n")
	} else {
		fmt.Fprintf(os.Stderr, "[codebuddy] could not click Log in tab, trying Google button directly\n")
	}

	randomDelay(800, 1500)

	googleURL := ""
	for attempt := 0; attempt < 8; attempt++ {
		result, err := page.Eval(`() => {
			for (const a of document.querySelectorAll('a[href*="/broker/google/login"]')) {
				if (a.offsetParent !== null) {
					return a.href;
				}
			}
			const phrases = ['log in with google', 'sign in with google', 'sign up with google', 'continue with google'];
			for (const el of document.querySelectorAll('a')) {
				if (el.offsetParent === null) continue;
				const txt = (el.textContent || '').toLowerCase().trim();
				if (phrases.some(p => txt.includes(p)) && el.href) {
					return el.href;
				}
			}
			return '';
		}`)
		if err == nil && result != nil {
			googleURL = result.Value.Str()
		}
		if googleURL != "" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if googleURL == "" {
		links, _ := page.Eval(`() => {
			const result = [];
			document.querySelectorAll('a').forEach(a => {
				result.push({href: a.href, text: (a.textContent||'').trim().substring(0,50)});
			});
			return JSON.stringify(result.slice(0, 10));
		}`)
		if links != nil {
			fmt.Fprintf(os.Stderr, "[codebuddy] available links: %s\n", links.Value.Str())
		}
		return fmt.Errorf("could not find Google login button on codebuddy page")
	}

	fmt.Fprintf(os.Stderr, "[codebuddy] navigating to Google broker: %s\n", googleURL[:minInt(80, len(googleURL))])
	if err := page.Navigate(googleURL); err != nil {
		return fmt.Errorf("failed to navigate to Google broker URL: %w", err)
	}
	_ = page.Timeout(15 * time.Second).WaitLoad()

	return nil
}