package main

// stealth_useragent.go — Lock UA to actual Chromium version
// Detects real Chromium version from browser and locks all UA-related APIs to match.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// ChromiumVersion holds parsed version info from the actual browser binary.
type ChromiumVersion struct {
	Full     string // e.g. "136.0.7103.92"
	Major    string // e.g. "136"
	Platform string // e.g. "Windows NT 10.0; Win64; x64"
}

var versionRegex = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)\.(\d+)`)

// DetectChromiumVersion queries the browser for its actual version.
func DetectChromiumVersion(browser *rod.Browser) (ChromiumVersion, error) {
	ver, err := proto.BrowserGetVersion{}.Call(browser)
	if err != nil {
		return ChromiumVersion{}, fmt.Errorf("BrowserGetVersion: %w", err)
	}

	// ver.Product is like "Chrome/136.0.7103.92"
	parts := strings.SplitN(ver.Product, "/", 2)
	version := ""
	if len(parts) == 2 {
		version = parts[1]
	}

	major := ""
	if m := versionRegex.FindStringSubmatch(version); len(m) > 1 {
		major = m[1]
	}

	// Extract platform from userAgent string
	platform := "Windows NT 10.0; Win64; x64" // default
	if strings.Contains(ver.UserAgent, "(") {
		start := strings.Index(ver.UserAgent, "(")
		end := strings.Index(ver.UserAgent, ")")
		if start >= 0 && end > start {
			platform = ver.UserAgent[start+1 : end]
		}
	}

	return ChromiumVersion{
		Full:     version,
		Major:    major,
		Platform: platform,
	}, nil
}

// BuildLockedUA constructs a UA string locked to the actual Chromium version.
func BuildLockedUA(cv ChromiumVersion, spoofPlatform string) string {
	platform := cv.Platform
	if spoofPlatform != "" {
		platform = spoofPlatform
	}
	return fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		platform, cv.Full,
	)
}

// ApplyLockedUA sets the UA at launcher level and patches all JS APIs to match.
func ApplyLockedUA(l *launcher.Launcher, cv ChromiumVersion, spoofPlatform string) *launcher.Launcher {
	ua := BuildLockedUA(cv, spoofPlatform)
	l = l.Set("user-agent", ua)
	return l
}

// ApplyUAConsistency injects JS to ensure navigator.userAgent, userAgentData,
// and platform all match the locked UA.
func ApplyUAConsistency(page *rod.Page, cv ChromiumVersion, spoofPlatform string) error {
	ua := BuildLockedUA(cv, spoofPlatform)

	platform := "Win32"
	if strings.Contains(spoofPlatform, "Macintosh") || strings.Contains(cv.Platform, "Macintosh") {
		platform = "MacIntel"
	} else if strings.Contains(spoofPlatform, "Linux") || strings.Contains(cv.Platform, "Linux") {
		platform = "Linux x86_64"
	}

	script := fmt.Sprintf(stealthUAConsistencyTemplate, ua, platform, cv.Major, cv.Full)
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
	return err
}

// stealthUAConsistencyTemplate — %s = full UA, %s = platform, %s = major version, %s = full version
const stealthUAConsistencyTemplate = `(() => {
	const UA = '%s';
	const PLATFORM = '%s';
	const MAJOR = '%s';
	const FULL_VERSION = '%s';

	// Lock navigator.userAgent
	Object.defineProperty(navigator, 'userAgent', {
		get: () => UA,
		configurable: true
	});

	// Lock navigator.platform
	Object.defineProperty(navigator, 'platform', {
		get: () => PLATFORM,
		configurable: true
	});

	// Lock navigator.appVersion
	Object.defineProperty(navigator, 'appVersion', {
		get: () => UA.replace('Mozilla/', ''),
		configurable: true
	});

	// Lock navigator.userAgentData (Chrome 90+)
	if (navigator.userAgentData) {
		const brands = [
			{ brand: 'Chromium', version: MAJOR },
			{ brand: 'Google Chrome', version: MAJOR },
			{ brand: 'Not-A.Brand', version: '99' }
		];
		Object.defineProperty(navigator, 'userAgentData', {
			get: () => ({
				brands: brands,
				mobile: false,
				platform: PLATFORM.includes('Win') ? 'Windows' : 
				          PLATFORM.includes('Mac') ? 'macOS' : 'Linux',
				getHighEntropyValues: (hints) => Promise.resolve({
					brands: brands,
					fullVersionList: [
						{ brand: 'Chromium', version: FULL_VERSION },
						{ brand: 'Google Chrome', version: FULL_VERSION },
						{ brand: 'Not-A.Brand', version: '99.0.0.0' }
					],
					mobile: false,
					model: '',
					platform: PLATFORM.includes('Win') ? 'Windows' : 
					          PLATFORM.includes('Mac') ? 'macOS' : 'Linux',
					platformVersion: PLATFORM.includes('Win') ? '15.0.0' : '13.0.0',
					architecture: 'x86',
					bitness: '64',
					uaFullVersion: FULL_VERSION
				}),
				toJSON: function() {
					return { brands: brands, mobile: false, platform: this.platform };
				}
			}),
			configurable: true
		});
	}

	// Lock navigator.vendor
	Object.defineProperty(navigator, 'vendor', {
		get: () => 'Google Inc.',
		configurable: true
	});

	// Lock navigator.vendorSub
	Object.defineProperty(navigator, 'vendorSub', {
		get: () => '',
		configurable: true
	});
})()`;
