package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Common desktop resolutions for randomization
var commonResolutions = [][2]int{
	{1920, 1080},
	{1366, 768},
	{1440, 900},
	{1536, 864},
	{1680, 1050},
	{1280, 720},
	{1600, 900},
	{2560, 1440},
}

// --- Layer 1-6: Launcher-level stealth ---

func stealthLauncherFlags(l *launcher.Launcher) *launcher.Launcher {
	// Layer 1: Disable automation indicators
	l = l.Set("disable-blink-features", "AutomationControlled")
	l = l.Delete("enable-automation")

	// Layer 2: Realistic window size
	res := commonResolutions[rand.Intn(len(commonResolutions))]
	l = l.Set("window-size", fmt.Sprintf("%d,%d", res[0], res[1]))

	// Layer 3: WebRTC leak prevention
	l = l.Set("enforce-webrtc-ip-handling-policy", "")
	l = l.Set("webrtc-ip-handling-policy", "disable_non_proxied_udp")

	// Layer 4: GPU/WebGL — handled by gpu_offload.go (ApplyGPUOffload)
	// Removed: use-gl and use-angle are set based on GPU_MODE env

	// Layer 5: Disable telltale infobars and popups
	l = l.Set("disable-infobars", "")
	l = l.Set("no-first-run", "")
	l = l.Set("disable-popup-blocking", "")
	l = l.Set("disable-background-networking", "")
	l = l.Set("disable-client-side-phishing-detection", "")
	l = l.Set("disable-default-apps", "")
	l = l.Set("disable-extensions", "")
	l = l.Set("disable-hang-monitor", "")
	l = l.Set("disable-prompt-on-repost", "")
	l = l.Set("disable-sync", "")
	l = l.Set("metrics-recording-only", "")
	l = l.Set("no-default-browser-check", "")

	// Layer 6: User agent override (modern Chrome on Windows)
	l = l.Set("user-agent", randomUserAgent())

	return l
}

func randomUserAgent() string {
	// Recent Chrome versions on Windows 10/11
	versions := []string{
		"124.0.6367.91",
		"124.0.6367.78",
		"123.0.6312.122",
		"123.0.6312.106",
		"122.0.6261.128",
		"125.0.6422.60",
	}
	platforms := []string{
		"Windows NT 10.0; Win64; x64",
		"Windows NT 10.0; Win64; x64",
		"Macintosh; Intel Mac OS X 10_15_7",
		"X11; Linux x86_64",
	}
	v := versions[rand.Intn(len(versions))]
	p := platforms[rand.Intn(len(platforms))]
	return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", p, v)
}

// --- Layer 7-18: CDP JavaScript injection ---

func applyStealth(page *rod.Page) error {
	scripts := []string{
		// Layer 7: navigator.webdriver = false
		stealthWebdriver,
		// Layer 8: navigator.plugins
		stealthPlugins,
		// Layer 9: navigator.languages
		stealthLanguages,
		// Layer 10: chrome.runtime
		stealthChromeRuntime,
		// Layer 11: permissions.query
		stealthPermissions,
		// Layer 12: WebGL renderer/vendor
		stealthWebGL,
		// Layer 13: Canvas fingerprint noise
		stealthCanvas,
		// Layer 14: AudioContext fingerprint
		stealthAudio,
		// Layer 15: Battery API removal
		stealthBattery,
		// Layer 16: Screen/display spoofing
		stealthScreen,
		// Layer 17: iframe contentWindow
		stealthIframe,
		// Layer 18: Hairline / CSS.supports
		stealthHairline,
	}

	for _, script := range scripts {
		_, err := page.Eval(script)
		if err != nil {
			return fmt.Errorf("stealth injection failed: %w", err)
		}
	}

	return nil
}

// applyStealthViaAddScript uses CDP to inject scripts that run before page JS.
// This is more robust than Eval for pages that check early.
func applyStealthViaAddScript(page *rod.Page) error {
	scripts := []string{
		stealthWebdriver,
		stealthPlugins,
		stealthLanguages,
		stealthChromeRuntime,
		stealthPermissions,
		stealthWebGL,
		stealthCanvas,
		stealthAudio,
		stealthBattery,
		stealthScreen,
		stealthIframe,
		stealthHairline,
	}

	for _, script := range scripts {
		_, _ = proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
	}
	return nil
}

// Layer 7
const stealthWebdriver = `(() => {
	// Handled by stealth_hardened.go (hardenedPropertyDescriptors)
	// which puts webdriver on Navigator.prototype with native-looking getter.
	// This is a fallback in case hardened layer didn't run first.
	if (typeof window.__nativeProperty === 'function') {
		// Use hardened helper — produces native toString()
		try { delete navigator.webdriver; } catch(e) {}
		try { delete Navigator.prototype.webdriver; } catch(e) {}
		window.__nativeProperty(Navigator.prototype, 'webdriver', () => false);
	} else {
		// Fallback (less stealthy)
		Object.defineProperty(navigator, 'webdriver', {
			get: () => false,
			configurable: true
		});
		delete Object.getPrototypeOf(navigator).webdriver;
	}
})()`

// Layer 8
const stealthPlugins = `(() => {
	const makePlugin = (name, desc, filename, mimeType) => {
		const mime = { type: mimeType, suffixes: '', description: desc, enabledPlugin: null };
		const plugin = { name, description: desc, filename, length: 1, 0: mime };
		mime.enabledPlugin = plugin;
		return plugin;
	};
	const plugins = [
		makePlugin('Chrome PDF Plugin', 'Portable Document Format', 'internal-pdf-viewer', 'application/x-google-chrome-pdf'),
		makePlugin('Chrome PDF Viewer', '', 'mhjfbmdgcfjbbpaeojofohoefgiehjai', 'application/pdf'),
		makePlugin('Native Client', '', 'internal-nacl-plugin', 'application/x-nacl'),
	];
	Object.defineProperty(navigator, 'plugins', {
		get: () => {
			const arr = plugins;
			arr.item = i => arr[i];
			arr.namedItem = n => arr.find(p => p.name === n);
			arr.refresh = () => {};
			return arr;
		},
		configurable: true
	});
	Object.defineProperty(navigator, 'mimeTypes', {
		get: () => {
			const mimes = plugins.map(p => p[0]);
			mimes.item = i => mimes[i];
			mimes.namedItem = n => mimes.find(m => m.type === n);
			return mimes;
		},
		configurable: true
	});
})()`

// Layer 9
const stealthLanguages = `(() => {
	Object.defineProperty(navigator, 'languages', {
		get: () => ['en-US', 'en'],
		configurable: true
	});
	Object.defineProperty(navigator, 'language', {
		get: () => 'en-US',
		configurable: true
	});
})()`

// Layer 10
const stealthChromeRuntime = `(() => {
	if (!window.chrome) window.chrome = {};
	if (!window.chrome.runtime) {
		window.chrome.runtime = {
			connect: function() { return { onMessage: { addListener: function(){} }, postMessage: function(){}, disconnect: function(){} }; },
			sendMessage: function(msg, cb) { if (cb) cb(); },
			onMessage: { addListener: function(){}, removeListener: function(){} },
			onConnect: { addListener: function(){}, removeListener: function(){} },
			id: undefined
		};
	}
	window.chrome.app = { isInstalled: false, InstallState: { INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' }, RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' } };
	window.chrome.csi = function() { return { startE: Date.now(), onloadT: Date.now() + 100, pageT: Date.now() + 200, tran: 15 }; };
	window.chrome.loadTimes = function() { return { requestTime: Date.now() / 1000, startLoadTime: Date.now() / 1000, commitLoadTime: Date.now() / 1000 + 0.1, finishDocumentLoadTime: Date.now() / 1000 + 0.2, finishLoadTime: Date.now() / 1000 + 0.3, firstPaintTime: Date.now() / 1000 + 0.1, firstPaintAfterLoadTime: 0, navigationType: 'Other', wasFetchedViaSpdy: false, wasNpnNegotiated: true, npnNegotiatedProtocol: 'h2', wasAlternateProtocolAvailable: false, connectionInfo: 'h2' }; };
})()`

// Layer 11
const stealthPermissions = `(() => {
	const originalQuery = window.navigator.permissions.query.bind(window.navigator.permissions);
	const patchedQuery = (parameters) => {
		if (parameters.name === 'notifications') {
			return Promise.resolve({ state: 'default' });
		}
		return originalQuery(parameters);
	};
	if (typeof window.__markNative === 'function') {
		window.__markNative(patchedQuery, 'query');
	}
	window.navigator.permissions.query = patchedQuery;
})()`

// Layer 12
const stealthWebGL = `(() => {
	const getParameter = WebGLRenderingContext.prototype.getParameter;
	const patchedGetParam = function(parameter) {
		if (parameter === 37445) return 'Google Inc. (Intel)';
		if (parameter === 37446) return 'ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)';
		return getParameter.call(this, parameter);
	};
	if (typeof window.__markNative === 'function') {
		window.__markNative(patchedGetParam, 'getParameter');
	}
	WebGLRenderingContext.prototype.getParameter = patchedGetParam;

	const getParameter2 = WebGL2RenderingContext.prototype.getParameter;
	const patchedGetParam2 = function(parameter) {
		if (parameter === 37445) return 'Google Inc. (Intel)';
		if (parameter === 37446) return 'ANGLE (Intel, Intel(R) UHD Graphics 630 Direct3D11 vs_5_0 ps_5_0, D3D11)';
		return getParameter2.call(this, parameter);
	};
	if (typeof window.__markNative === 'function') {
		window.__markNative(patchedGetParam2, 'getParameter');
	}
	WebGL2RenderingContext.prototype.getParameter = patchedGetParam2;
})()`

// Layer 13
const stealthCanvas = `(() => {
	const seed = Math.floor(Math.random() * 1000);
	const origToDataURL = HTMLCanvasElement.prototype.toDataURL;
	HTMLCanvasElement.prototype.toDataURL = function(type) {
		const ctx = this.getContext('2d');
		if (ctx) {
			const imageData = ctx.getImageData(0, 0, Math.min(this.width, 2), Math.min(this.height, 2));
			for (let i = 0; i < imageData.data.length; i += 4) {
				imageData.data[i] = imageData.data[i] ^ (seed & 0x1);
			}
			ctx.putImageData(imageData, 0, 0);
		}
		return origToDataURL.apply(this, arguments);
	};
	const origToBlob = HTMLCanvasElement.prototype.toBlob;
	HTMLCanvasElement.prototype.toBlob = function() {
		const ctx = this.getContext('2d');
		if (ctx) {
			const imageData = ctx.getImageData(0, 0, Math.min(this.width, 2), Math.min(this.height, 2));
			for (let i = 0; i < imageData.data.length; i += 4) {
				imageData.data[i] = imageData.data[i] ^ (seed & 0x1);
			}
			ctx.putImageData(imageData, 0, 0);
		}
		return origToBlob.apply(this, arguments);
	};
})()`

// Layer 14
const stealthAudio = `(() => {
	const ctx = window.AudioContext || window.webkitAudioContext;
	if (!ctx) return;
	const origCreateOscillator = ctx.prototype.createOscillator;
	ctx.prototype.createOscillator = function() {
		const osc = origCreateOscillator.call(this);
		osc._stealthNoise = (Math.random() - 0.5) * 0.0001;
		return osc;
	};
	const origGetFloatFrequencyData = AnalyserNode.prototype.getFloatFrequencyData;
	AnalyserNode.prototype.getFloatFrequencyData = function(array) {
		origGetFloatFrequencyData.call(this, array);
		for (let i = 0; i < array.length; i++) {
			array[i] += (Math.random() - 0.5) * 0.1;
		}
	};
})()`

// Layer 15
const stealthBattery = `(() => {
	delete navigator.getBattery;
	Object.defineProperty(navigator, 'getBattery', { get: () => undefined, configurable: true });
})()`

// Layer 16
const stealthScreen = `(() => {
	const w = window.innerWidth || 1920;
	const h = window.innerHeight || 1080;
	Object.defineProperty(screen, 'width', { get: () => w, configurable: true });
	Object.defineProperty(screen, 'height', { get: () => h, configurable: true });
	Object.defineProperty(screen, 'availWidth', { get: () => w, configurable: true });
	Object.defineProperty(screen, 'availHeight', { get: () => h - 40, configurable: true });
	Object.defineProperty(screen, 'colorDepth', { get: () => 24, configurable: true });
	Object.defineProperty(screen, 'pixelDepth', { get: () => 24, configurable: true });
})()`

// Layer 17
const stealthIframe = `(() => {
	const origContentWindow = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, 'contentWindow');
	Object.defineProperty(HTMLIFrameElement.prototype, 'contentWindow', {
		get: function() {
			const win = origContentWindow.get.call(this);
			if (win) {
				try {
					Object.defineProperty(win.navigator, 'webdriver', { get: () => undefined, configurable: true });
				} catch(e) {}
			}
			return win;
		},
		configurable: true
	});
})()`

// Layer 18
const stealthHairline = `(() => {
	if (!window.CSS || !window.CSS.supports) return;
	const origSupports = window.CSS.supports;
	window.CSS.supports = function() {
		if (arguments.length === 1 && arguments[0].includes('-webkit-')) return true;
		return origSupports.apply(this, arguments);
	};
	// Ensure matchMedia reports accurate device pixel ratio
	Object.defineProperty(window, 'devicePixelRatio', { get: () => 1, configurable: true });
})()`

// --- Layer 19-22: Behavioral stealth helpers ---

// Layer 19: Human-like typing with random inter-key delays
func humanType(page *rod.Page, el *rod.Element, text string) {
	// Clear existing text (non-panic version)
	_ = el.SelectAllText()
	time.Sleep(100 * time.Millisecond)
	for _, ch := range text {
		_ = el.Input(string(ch))
		delay := 50 + rand.Intn(100) // 50-150ms per keystroke
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

// Layer 20: Mouse movement before click (bezier curve simulation)
func humanClick(page *rod.Page, el *rod.Element) error {
	shape, err := el.Shape()
	if err != nil {
		// Fallback to direct click
		return el.Click(proto.InputMouseButtonLeft, 1)
	}

	// Get center of element
	box := shape.Box()
	targetX := box.X + box.Width/2 + (rand.Float64()-0.5)*box.Width*0.3
	targetY := box.Y + box.Height/2 + (rand.Float64()-0.5)*box.Height*0.3

	// Get current mouse position (start from random viewport position)
	startX := rand.Float64() * 200
	startY := rand.Float64() * 200

	// Move mouse in steps with bezier-like curve
	steps := 8 + rand.Intn(8) // 8-15 steps
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		// Ease-in-out cubic
		t = t * t * (3 - 2*t)

		// Add slight curve offset
		curveOffset := (1 - t) * t * (rand.Float64()*40 - 20)

		x := startX + (targetX-startX)*t + curveOffset
		y := startY + (targetY-startY)*t + curveOffset*0.5

		_ = proto.InputDispatchMouseEvent{
			Type: proto.InputDispatchMouseEventTypeMouseMoved,
			X:    x,
			Y:    y,
		}.Call(page)

		time.Sleep(time.Duration(10+rand.Intn(20)) * time.Millisecond)
	}

	// Small pause before click
	time.Sleep(time.Duration(30+rand.Intn(70)) * time.Millisecond)

	// Click at final position
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// Layer 21: Random micro-delay between actions
func randomDelay(minMs, maxMs int) {
	delay := minMs + rand.Intn(maxMs-minMs)
	time.Sleep(time.Duration(delay) * time.Millisecond)
}

// Layer 22: Random viewport scroll noise
func scrollNoise(page *rod.Page) {
	scrollY := rand.Intn(80) - 40 // -40 to +40 pixels
	if scrollY == 0 {
		return
	}
	page.Eval(fmt.Sprintf(`window.scrollBy(0, %d)`, scrollY))
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
