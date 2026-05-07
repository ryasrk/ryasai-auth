package main

// stealth_orchestrator.go — Orchestrates all stealth layers into a unified pipeline.
// Call ApplyFullStealth() after browser launch to apply everything.

import (
	"log"

	"github.com/go-rod/rod"
)

// StealthConfig holds all configurable stealth parameters.
type StealthConfig struct {
	Timezone   TimezoneProfile
	Connection ConnectionProfile
	TLS        TLSProfile
	WSTiming   WebSocketTimingConfig
	// If empty, will be detected from browser
	SpoofPlatform string
}

// DefaultStealthConfig generates a randomized stealth configuration.
func DefaultStealthConfig() StealthConfig {
	return StealthConfig{
		Timezone:      RandomTimezoneProfile(),
		Connection:    RandomConnectionProfile(),
		TLS:           RandomTLSProfile(),
		WSTiming:      DefaultWSTimingConfig(),
		SpoofPlatform: "Windows NT 10.0; Win64; x64",
	}
}

// ApplyFullStealthToPage applies all stealth layers to a page.
// Call this after page creation but before navigation.
func ApplyFullStealthToPage(browser *rod.Browser, page *rod.Page, cfg StealthConfig) error {
	// 0. HARDENED LAYER (MUST BE FIRST) — patches detection mechanisms themselves
	//    - Function.prototype.toString spoofing (makes all overrides look native)
	//    - Proxy detection shield
	//    - CDP timing side-channel normalization
	//    - Chrome headless tells (chrome.app, csi, loadTimes, etc.)
	//    - Native-level property descriptors
	//    - Iframe isolation
	if err := ApplyHardenedStealth(page); err != nil {
		log.Printf("[stealth] hardened layer warning: %v", err)
	}

	// 1. go-rod/stealth integration (CDP leak patches)
	page, err := ApplyRodStealth(page)
	if err != nil {
		log.Printf("[stealth] go-rod/stealth warning: %v", err)
		// Non-fatal, continue with other layers
	}

	// 2. CDP protocol cloaking (execution context leaks)
	if err := ApplyCDPCloaking(page); err != nil {
		log.Printf("[stealth] CDP cloaking warning: %v", err)
	}

	// 3. Detect actual Chromium version and lock UA
	cv, err := DetectChromiumVersion(browser)
	if err != nil {
		log.Printf("[stealth] version detection failed, using fallback: %v", err)
		cv = ChromiumVersion{Full: "136.0.7103.92", Major: "136", Platform: cfg.SpoofPlatform}
	}
	if err := ApplyUAConsistency(page, cv, cfg.SpoofPlatform); err != nil {
		log.Printf("[stealth] UA consistency warning: %v", err)
	}

	// 4. Timezone spoofing
	if err := ApplyTimezoneSpoof(page, cfg.Timezone); err != nil {
		log.Printf("[stealth] timezone spoof warning: %v", err)
	}

	// 5. Font enumeration masking
	if err := ApplyFontMasking(page); err != nil {
		log.Printf("[stealth] font masking warning: %v", err)
	}

	// 6. navigator.connection spoofing
	if err := ApplyConnectionSpoof(page, cfg.Connection); err != nil {
		log.Printf("[stealth] connection spoof warning: %v", err)
	}

	// 7. WebSocket frame timing normalization
	if err := ApplyWebSocketTimingNormalization(page, cfg.WSTiming); err != nil {
		log.Printf("[stealth] WS timing warning: %v", err)
	}

	return nil
}

// Note: Launcher-level stealth (TLS, timezone flags) is applied directly
// in launchBrowser() in main.go before l.Launch() is called.
