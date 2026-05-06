package main

import (
	"fmt"
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

	// Apply stealth JS injections (layers 7-18)
	applyStealthViaAddScript(page)
	applyStealth(page)

	// Apply enhanced stealth (CDP cloaking, timezone, fonts, connection, WS timing)
	cfg := DefaultStealthConfig()
	if err := ApplyFullStealthToPage(browser, page, cfg); err != nil {
		// Non-fatal: log and continue
		_ = err
	}

	// Wait for Google login page to load
	if err := waitForGoogleLogin(page); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	// Fill email
	if err := fillEmail(page, req.Email); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "email step: " + err.Error()}
	}

	// Wait for password step
	if err := waitForPasswordStep(page); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	// Fill password
	if err := fillPassword(page, req.Password); err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: "password step: " + err.Error()}
	}

	// Wait for redirect back to provider
	cookies, err := waitForProviderRedirect(page, req)
	if err != nil {
		return LoginResult{Status: "failed", Cookies: map[string]string{}, Error: err.Error()}
	}

	return LoginResult{Status: "success", Cookies: cookies, Error: ""}
}

func waitForGoogleLogin(page *rod.Page) error {
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		url := page.MustInfo().URL
		if strings.Contains(url, "accounts.google.com") {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for google login page")
}

func fillEmail(page *rod.Page, email string) error {
	// Wait for email input
	el, err := page.Timeout(15 * time.Second).Element(`input[type="email"], input[name="identifier"], #identifierId`)
	if err != nil {
		return fmt.Errorf("email input not found: %w", err)
	}

	// Layer 22: scroll noise before interaction
	scrollNoise(page)
	randomDelay(200, 600)

	// Layer 19: human-like typing
	humanType(page, el, email)
	randomDelay(300, 800)

	// Click Next
	if err := clickNextButton(page); err != nil {
		// Try pressing Enter as fallback
		page.Keyboard.MustType(input.Enter)
	}

	return nil
}

func waitForPasswordStep(page *rod.Page) error {
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)

		// Check for challenge/error pages
		url := page.MustInfo().URL
		if strings.Contains(url, "/challenge/") && !strings.Contains(url, "/challenge/pwd") {
			return fmt.Errorf("google challenge detected (captcha/2fa): %s", url)
		}

		// Check if password input is visible
		has, _, _ := page.Has(`input[type="password"], input[name="Passwd"]`)
		if has {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for password step")
}

func fillPassword(page *rod.Page, password string) error {
	el, err := page.Timeout(10 * time.Second).Element(`input[type="password"], input[name="Passwd"]`)
	if err != nil {
		return fmt.Errorf("password input not found: %w", err)
	}

	// Layer 22: scroll noise
	scrollNoise(page)
	randomDelay(200, 500)

	// Layer 19: human-like typing
	humanType(page, el, password)
	randomDelay(400, 900)

	// Layer 20: human-like click on Next
	if err := clickNextButton(page); err != nil {
		page.Keyboard.MustType(input.Enter)
	}

	return nil
}

func clickNextButton(page *rod.Page) error {
	// Try standard Google Next buttons
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
			// Layer 20: human-like mouse movement + click
			randomDelay(100, 300)
			return humanClick(page, el)
		}
	}

	return fmt.Errorf("no next button found")
}

func waitForProviderRedirect(page *rod.Page, req LoginRequest) (map[string]string, error) {
	providerHost := extractHost(req.TargetURL)

	for i := 0; i < 60; i++ {
		time.Sleep(1 * time.Second)

		url := page.MustInfo().URL

		// Handle consent screens
		if strings.Contains(url, "/oauthchooseaccount") ||
			strings.Contains(url, "/consent") ||
			strings.Contains(url, "/gaplustos") {
			handleConsentScreen(page)
			continue
		}

		// Check if we're back on provider
		if providerHost != "" && strings.Contains(url, providerHost) {
			return extractCookies(page, providerHost)
		}
	}

	return nil, fmt.Errorf("timeout waiting for redirect to %s", providerHost)
}

func handleConsentScreen(page *rod.Page) {
	randomDelay(500, 1500)

	// Try clicking Allow/Continue/Accept buttons
	selectors := []string{
		`#submit_approve_access`,
		`button[id="submit_approve_access"]`,
		`input[type="submit"]`,
		`#confirm`,
	}

	for _, sel := range selectors {
		has, el, _ := page.Has(sel)
		if has && el != nil {
			humanClick(page, el)
			randomDelay(800, 1500)
			return
		}
	}

	// Fallback: try any button with allow/continue text
	buttons, _ := page.Elements(`button, input[type="submit"], div[role="button"]`)
	for _, btn := range buttons {
		text, _ := btn.Text()
		lower := strings.ToLower(text)
		if strings.Contains(lower, "allow") ||
			strings.Contains(lower, "continue") ||
			strings.Contains(lower, "accept") ||
			strings.Contains(lower, "agree") {
			humanClick(page, btn)
			randomDelay(800, 1500)
			return
		}
	}
}

func extractCookies(page *rod.Page, host string) (map[string]string, error) {
	cookies, err := page.Cookies(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}

	result := make(map[string]string)
	for _, c := range cookies {
		if strings.Contains(c.Domain, host) || isTokenCookie(c.Name) {
			result[c.Name] = c.Value
		}
	}

	if len(result) == 0 {
		return result, fmt.Errorf("no provider cookies found after redirect")
	}

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
	// Simple host extraction
	s := rawURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.IndexAny(s, "/?#"); idx >= 0 {
		s = s[:idx]
	}
	// Remove port
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	// Get main domain (last 2 parts)
	parts := strings.Split(s, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return s
}
