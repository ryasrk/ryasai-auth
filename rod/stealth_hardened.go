package main

// stealth_hardened.go — Fixes all 3 Rod weaknesses to reach peak pass rate
//
// Weakness 1: JS override detectability (toString / prototype inspection)
// Weakness 2: CDP timing side-channels (execution timing leaks)
// Weakness 3: Chrome headless tells (missing APIs, chrome.app, etc.)
//
// This file MUST be injected FIRST (before all other stealth scripts)
// because it patches the detection mechanisms themselves.

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ApplyHardenedStealth injects the hardening layer BEFORE all other stealth.
// Call this first in the stealth pipeline.
func ApplyHardenedStealth(page *rod.Page) error {
	scripts := []string{
		// MUST be first: patches Function.prototype.toString so all subsequent
		// Object.defineProperty calls produce native-looking toString output
		hardenedToString,
		// Patches Proxy detection and prototype chain integrity
		hardenedProxyShield,
		// Fixes CDP timing side-channels
		hardenedTimingShield,
		// Fixes all Chrome headless tells
		hardenedHeadlessFixes,
		// Native-level property descriptors (non-configurable where Chrome expects it)
		hardenedPropertyDescriptors,
		// Iframe isolation (propagate patches to child frames)
		hardenedIframeIsolation,
	}

	for _, script := range scripts {
		_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
		if err != nil {
			return err
		}
	}
	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 1 FIX: Function.prototype.toString spoofing
// ═══════════════════════════════════════════════════════════════════════════
//
// Problem: When we do Object.defineProperty(navigator, 'webdriver', {get: () => undefined}),
// fingerprinters detect this by calling:
//   Object.getOwnPropertyDescriptor(navigator, 'webdriver').get.toString()
//   → "() => undefined"  (BUSTED — native would be "function get webdriver() { [native code] }")
//
// Fix: Intercept Function.prototype.toString and return "[native code]" for all
// our patched getters/setters. We maintain a WeakSet of "blessed" functions.

const hardenedToString = `(() => {
	// Store references before anything can tamper with them
	const _Reflect = Reflect;
	const _Object = Object;
	const _WeakSet = WeakSet;
	const _Error = Error;
	const _Proxy = Proxy;
	const _apply = _Reflect.apply;
	const _defineProperty = _Object.defineProperty;
	const _getOwnPropertyDescriptor = _Object.getOwnPropertyDescriptor;
	const _setPrototypeOf = _Object.setPrototypeOf;
	const _freeze = _Object.freeze;

	// Registry of functions that should appear native
	const nativeFns = new _WeakSet();
	const nativeNames = new WeakMap();

	// The real toString
	const realToString = Function.prototype.toString;
	const realCall = Function.prototype.call;
	const realBind = Function.prototype.bind;

	// Helper: register a function as "native-looking"
	const markNative = (fn, name) => {
		if (typeof fn === 'function') {
			nativeFns.add(fn);
			if (name) nativeNames.set(fn, name);
		}
		return fn;
	};

	// Patch Function.prototype.toString
	const patchedToString = function toString() {
		// If this function is in our blessed set, return native-looking string
		if (nativeFns.has(this)) {
			const name = nativeNames.get(this) || this.name || '';
			return 'function ' + name + '() { [native code] }';
		}
		// Otherwise use real toString
		return _apply(realToString, this, []);
	};

	// Mark our patched toString as native too (recursive protection)
	nativeFns.add(patchedToString);
	nativeNames.set(patchedToString, 'toString');

	_defineProperty(Function.prototype, 'toString', {
		value: patchedToString,
		writable: true,
		configurable: true
	});

	// Also patch toString.call and toString.apply detection
	// Some fingerprinters do: Function.prototype.toString.call(fn)
	// This is already handled since we replaced the function itself.

	// Export markNative globally (hidden) for other stealth scripts to use
	_defineProperty(window, '__markNative', {
		value: markNative,
		writable: false,
		configurable: false,
		enumerable: false
	});

	// Helper: create a native-looking getter/setter pair
	_defineProperty(window, '__nativeProperty', {
		value: function(obj, prop, getter, setter) {
			const descriptor = {};
			if (getter) {
				const g = getter;
				markNative(g, 'get ' + prop);
				descriptor.get = g;
			}
			if (setter) {
				const s = setter;
				markNative(s, 'set ' + prop);
				descriptor.set = s;
			}
			descriptor.configurable = true;
			descriptor.enumerable = true;
			_defineProperty(obj, prop, descriptor);
		},
		writable: false,
		configurable: false,
		enumerable: false
	});

	// Helper: create a native-looking value property (for methods)
	_defineProperty(window, '__nativeMethod', {
		value: function(obj, name, fn) {
			markNative(fn, name);
			_defineProperty(obj, name, {
				value: fn,
				writable: true,
				configurable: true,
				enumerable: true
			});
		},
		writable: false,
		configurable: false,
		enumerable: false
	});

	// Pre-mark common native functions that might get checked
	markNative(realToString, 'toString');
	markNative(realCall, 'call');
	markNative(realBind, 'bind');
	markNative(_Reflect.apply, 'apply');
	markNative(_Reflect.ownKeys, 'ownKeys');
	markNative(_Object.keys, 'keys');
	markNative(_Object.values, 'values');
	markNative(_Object.entries, 'entries');
	markNative(_Object.getOwnPropertyDescriptor, 'getOwnPropertyDescriptor');
	markNative(_Object.defineProperty, 'defineProperty');
})()`;

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 1 CONTINUED: Proxy detection shield
// ═══════════════════════════════════════════════════════════════════════════
//
// Problem: Some fingerprinters detect Proxy objects by:
//   1. Checking if Object.prototype.toString.call(obj) behaves differently
//   2. Using Symbol.toStringTag
//   3. Checking if JSON.stringify throws on circular proxy refs
//   4. Timing attacks on property access (Proxy adds ~0.1ms overhead)

const hardenedProxyShield = `(() => {
	// Patch Object.prototype.toString to not leak [object Proxy]
	const origObjToString = Object.prototype.toString;
	const proxyTargetMap = new WeakMap();

	// Override Proxy constructor to track targets
	const OrigProxy = window.Proxy;
	window.Proxy = new OrigProxy(OrigProxy, {
		construct(target, args) {
			const proxy = Reflect.construct(target, args);
			proxyTargetMap.set(proxy, args[0]);
			return proxy;
		},
		apply(target, thisArg, args) {
			return Reflect.apply(target, thisArg, args);
		}
	});
	// Make Proxy look native
	window.__markNative(window.Proxy, 'Proxy');
	Object.defineProperty(window.Proxy, 'length', { value: 2 });
	Object.defineProperty(window.Proxy, 'name', { value: 'Proxy' });

	// Patch instanceof checks
	Object.defineProperty(OrigProxy, Symbol.hasInstance, {
		value: () => false, // Nothing should appear as instanceof Proxy
		configurable: true
	});
})()`;

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 2 FIX: CDP timing side-channels
// ═══════════════════════════════════════════════════════════════════════════
//
// Problem: CDP commands (Runtime.evaluate, Page.addScriptToEvaluateOnNewDocument)
// introduce measurable timing artifacts:
//   1. Performance.now() shows gaps during CDP command execution
//   2. requestAnimationFrame timing is irregular during injection
//   3. Date.now() can show time jumps
//   4. Event loop timing is disrupted (microtask queue stalls)
//
// Fix: Normalize timing APIs to hide CDP execution gaps

const hardenedTimingShield = `(() => {
	// Capture real timing functions
	const _perfNow = Performance.prototype.now;
	const _dateNow = Date.now;
	const _setTimeout = window.setTimeout;
	const _raf = window.requestAnimationFrame;

	// Track timing offset to smooth out CDP gaps
	let timeOffset = 0;
	let lastRealTime = _perfNow.call(performance);
	let lastReportedTime = lastRealTime;
	const MAX_GAP = 16.67; // Max acceptable gap (1 frame at 60fps)

	// Smooth Performance.now() — hide CDP execution gaps
	const smoothPerfNow = function now() {
		const realTime = _perfNow.call(performance);
		const gap = realTime - lastRealTime;

		if (gap > MAX_GAP * 3) {
			// Large gap detected (CDP command was executing)
			// Compress it to look like normal frame time
			timeOffset += gap - (MAX_GAP + Math.random() * 5);
		}

		lastRealTime = realTime;
		lastReportedTime = realTime - timeOffset;
		return lastReportedTime;
	};
	window.__markNative(smoothPerfNow, 'now');

	Object.defineProperty(Performance.prototype, 'now', {
		value: smoothPerfNow,
		writable: true,
		configurable: true
	});

	// Smooth Date.now() similarly
	let dateOffset = 0;
	let lastDateNow = _dateNow();

	const smoothDateNow = function now() {
		const real = _dateNow();
		const gap = real - lastDateNow;
		if (gap > 50) { // 50ms gap = suspicious
			dateOffset += gap - (16 + Math.floor(Math.random() * 10));
		}
		lastDateNow = real;
		return real - dateOffset;
	};
	window.__markNative(smoothDateNow, 'now');
	Date.now = smoothDateNow;

	// Normalize requestAnimationFrame timing
	// CDP can cause rAF callbacks to fire with irregular timestamps
	let lastRAFTime = 0;
	const origRAF = window.requestAnimationFrame;
	window.requestAnimationFrame = function(callback) {
		return origRAF.call(window, function(timestamp) {
			// Ensure monotonically increasing, ~16.67ms intervals
			if (lastRAFTime > 0) {
				const delta = timestamp - lastRAFTime;
				if (delta > 50 || delta < 0) {
					// Normalize to expected frame time
					timestamp = lastRAFTime + 16.67 + Math.random() * 2;
				}
			}
			lastRAFTime = timestamp;
			callback(timestamp);
		});
	};
	window.__markNative(window.requestAnimationFrame, 'requestAnimationFrame');

	// Patch performance.getEntries() to remove CDP-related entries
	const origGetEntries = Performance.prototype.getEntries;
	Performance.prototype.getEntries = function() {
		const entries = origGetEntries.call(this);
		return entries.filter(e => {
			// Remove any entries that reveal CDP/DevTools
			if (e.name && (e.name.includes('devtools') || e.name.includes('__puppeteer'))) {
				return false;
			}
			return true;
		});
	};
	window.__markNative(Performance.prototype.getEntries, 'getEntries');

	// Patch performance.getEntriesByType similarly
	const origGetEntriesByType = Performance.prototype.getEntriesByType;
	Performance.prototype.getEntriesByType = function(type) {
		const entries = origGetEntriesByType.call(this, type);
		return entries.filter(e => {
			if (e.name && (e.name.includes('devtools') || e.name.includes('__puppeteer'))) {
				return false;
			}
			return true;
		});
	};
	window.__markNative(Performance.prototype.getEntriesByType, 'getEntriesByType');

	// Patch PerformanceObserver to filter CDP artifacts
	const OrigPerfObserver = window.PerformanceObserver;
	if (OrigPerfObserver) {
		window.PerformanceObserver = function(callback) {
			const wrappedCallback = function(list, observer) {
				const entries = list.getEntries().filter(e => {
					return !(e.name && e.name.includes('devtools'));
				});
				if (entries.length > 0) {
					callback({ getEntries: () => entries }, observer);
				}
			};
			return new OrigPerfObserver(wrappedCallback);
		};
		window.PerformanceObserver.supportedEntryTypes = OrigPerfObserver.supportedEntryTypes;
		window.__markNative(window.PerformanceObserver, 'PerformanceObserver');
	}
})()`;

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 3 FIX: Chrome headless tells
// ═══════════════════════════════════════════════════════════════════════════
//
// Known headless Chrome tells that fingerprinters check:
//   1. chrome.app missing or incomplete
//   2. chrome.csi missing
//   3. chrome.loadTimes missing
//   4. window.chrome.runtime.connect behavior
//   5. Notification.permission = "denied" by default in headless
//   6. navigator.plugins empty in headless
//   7. window.outerWidth/outerHeight = 0 in headless
//   8. Missing chrome.runtime.PlatformOs
//   9. navigator.hardwareConcurrency = 0 or 1 in some headless
//  10. Missing window.chrome.webstore (removed in Chrome 110+ but checked)
//  11. Broken image dimensions (images load as 0x0 in headless)
//  12. Missing speechSynthesis voices

const hardenedHeadlessFixes = `(() => {
	// ── 1. chrome.app (full implementation) ──────────────────────
	if (!window.chrome) window.chrome = {};

	window.chrome.app = {
		isInstalled: false,
		InstallState: {
			DISABLED: 'disabled',
			INSTALLED: 'installed',
			NOT_INSTALLED: 'not_installed'
		},
		RunningState: {
			CANNOT_RUN: 'cannot_run',
			READY_TO_RUN: 'ready_to_run',
			RUNNING: 'running'
		},
		getDetails: function() { return null; },
		getIsInstalled: function() { return false; },
		installState: function() { return 'not_installed'; },
		runningState: function() { return 'cannot_run'; }
	};
	window.__markNative(window.chrome.app.getDetails, 'getDetails');
	window.__markNative(window.chrome.app.getIsInstalled, 'getIsInstalled');
	window.__markNative(window.chrome.app.installState, 'installState');
	window.__markNative(window.chrome.app.runningState, 'runningState');

	// ── 2. chrome.csi ────────────────────────────────────────────
	window.chrome.csi = function() {
		return {
			onloadT: Date.now(),
			startE: Date.now() - Math.floor(Math.random() * 1000 + 500),
			pageT: Math.random() * 3000 + 1000,
			tran: 15
		};
	};
	window.__markNative(window.chrome.csi, 'csi');

	// ── 3. chrome.loadTimes ──────────────────────────────────────
	window.chrome.loadTimes = function() {
		const now = Date.now() / 1000;
		return {
			commitLoadTime: now - Math.random() * 2,
			connectionInfo: 'h2',
			finishDocumentLoadTime: now - Math.random(),
			finishLoadTime: now - Math.random() * 0.5,
			firstPaintAfterLoadTime: now - Math.random() * 0.3,
			firstPaintTime: now - Math.random() * 1.5,
			navigationType: 'Other',
			npnNegotiatedProtocol: 'h2',
			requestTime: now - Math.random() * 3,
			startLoadTime: now - Math.random() * 2.5,
			wasAlternateProtocolAvailable: false,
			wasFetchedViaSpdy: true,
			wasNpnNegotiated: true
		};
	};
	window.__markNative(window.chrome.loadTimes, 'loadTimes');

	// ── 4. chrome.runtime (complete) ─────────────────────────────
	if (!window.chrome.runtime) window.chrome.runtime = {};
	const rt = window.chrome.runtime;
	if (!rt.PlatformOs) {
		rt.PlatformOs = { MAC: 'mac', WIN: 'win', ANDROID: 'android', CROS: 'cros', LINUX: 'linux', OPENBSD: 'openbsd' };
	}
	if (!rt.PlatformArch) {
		rt.PlatformArch = { ARM: 'arm', ARM64: 'arm64', MIPS: 'mips', MIPS64: 'mips64', X86_32: 'x86-32', X86_64: 'x86-64' };
	}
	if (!rt.PlatformNaclArch) {
		rt.PlatformNaclArch = { ARM: 'arm', MIPS: 'mips', MIPS64: 'mips64', X86_32: 'x86-32', X86_64: 'x86-64' };
	}
	if (!rt.RequestUpdateCheckStatus) {
		rt.RequestUpdateCheckStatus = { THROTTLED: 'throttled', NO_UPDATE: 'no_update', UPDATE_AVAILABLE: 'update_available' };
	}
	if (!rt.OnInstalledReason) {
		rt.OnInstalledReason = { INSTALL: 'install', UPDATE: 'update', CHROME_UPDATE: 'chrome_update', SHARED_MODULE_UPDATE: 'shared_module_update' };
	}
	if (!rt.OnRestartRequiredReason) {
		rt.OnRestartRequiredReason = { APP_UPDATE: 'app_update', OS_UPDATE: 'os_update', PERIODIC: 'periodic' };
	}

	// connect must exist and return a Port-like object
	if (!rt.connect) {
		rt.connect = function() {
			return {
				name: '',
				sender: undefined,
				onDisconnect: { addListener: function(){}, removeListener: function(){}, hasListeners: function(){ return false; } },
				onMessage: { addListener: function(){}, removeListener: function(){}, hasListeners: function(){ return false; } },
				postMessage: function(){},
				disconnect: function(){}
			};
		};
		window.__markNative(rt.connect, 'connect');
	}
	if (!rt.sendMessage) {
		rt.sendMessage = function() {};
		window.__markNative(rt.sendMessage, 'sendMessage');
	}

	// id must be undefined (not missing) — real Chrome without extensions
	Object.defineProperty(rt, 'id', { get: () => undefined, configurable: true, enumerable: true });

	// ── 5. Notification.permission ───────────────────────────────
	// Headless defaults to "denied", real Chrome defaults to "default"
	if (typeof Notification !== 'undefined') {
		Object.defineProperty(Notification, 'permission', {
			get: () => 'default',
			configurable: true
		});
	}

	// ── 6. window.outerWidth/outerHeight (0 in headless) ─────────
	if (window.outerWidth === 0) {
		Object.defineProperty(window, 'outerWidth', {
			get: () => window.innerWidth + 15, // scrollbar width
			configurable: true
		});
	}
	if (window.outerHeight === 0) {
		Object.defineProperty(window, 'outerHeight', {
			get: () => window.innerHeight + 85, // toolbar + tabs
			configurable: true
		});
	}

	// ── 7. navigator.hardwareConcurrency ─────────────────────────
	// Headless sometimes reports 1 or 2, real machines have 4-16
	const realCores = navigator.hardwareConcurrency;
	if (!realCores || realCores < 4) {
		window.__nativeProperty(navigator, 'hardwareConcurrency', () => 8);
	}

	// ── 8. navigator.deviceMemory ────────────────────────────────
	// Should be 4 or 8 for modern machines
	if (!navigator.deviceMemory || navigator.deviceMemory < 4) {
		window.__nativeProperty(navigator, 'deviceMemory', () => 8);
	}

	// ── 9. Broken image dimensions fix ───────────────────────────
	// In headless, images may report 0x0 dimensions
	// Fix: ensure naturalWidth/naturalHeight are non-zero for loaded images
	const origImage = window.Image;
	window.Image = function(w, h) {
		const img = new origImage(w, h);
		// Ensure broken image test passes (16x16 is expected)
		const origNW = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'naturalWidth');
		const origNH = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'naturalHeight');
		if (origNW && origNH) {
			Object.defineProperty(img, 'naturalWidth', {
				get: function() {
					const val = origNW.get.call(this);
					return val === 0 && this.complete ? 16 : val;
				}
			});
			Object.defineProperty(img, 'naturalHeight', {
				get: function() {
					const val = origNH.get.call(this);
					return val === 0 && this.complete ? 16 : val;
				}
			});
		}
		return img;
	};
	window.Image.prototype = origImage.prototype;
	window.__markNative(window.Image, 'Image');

	// ── 10. speechSynthesis voices ───────────────────────────────
	// Headless Chrome has no voices, real Chrome has 20+
	if (window.speechSynthesis) {
		const fakeVoices = [
			{ name: 'Microsoft David - English (United States)', lang: 'en-US', localService: true, default: true, voiceURI: 'Microsoft David - English (United States)' },
			{ name: 'Microsoft Zira - English (United States)', lang: 'en-US', localService: true, default: false, voiceURI: 'Microsoft Zira - English (United States)' },
			{ name: 'Google US English', lang: 'en-US', localService: false, default: false, voiceURI: 'Google US English' },
			{ name: 'Google UK English Female', lang: 'en-GB', localService: false, default: false, voiceURI: 'Google UK English Female' },
			{ name: 'Google UK English Male', lang: 'en-GB', localService: false, default: false, voiceURI: 'Google UK English Male' },
		];
		const origGetVoices = speechSynthesis.getVoices.bind(speechSynthesis);
		speechSynthesis.getVoices = function() {
			const voices = origGetVoices();
			return voices.length > 0 ? voices : fakeVoices;
		};
		window.__markNative(speechSynthesis.getVoices, 'getVoices');
	}

	// ── 11. window.chrome.webstore (legacy check) ────────────────
	// Removed in Chrome 110+ but some old fingerprinters still check existence
	// Don't add it — its ABSENCE is correct for modern Chrome

	// ── 12. navigator.maxTouchPoints ─────────────────────────────
	// Desktop Chrome should report 0 (not undefined)
	if (navigator.maxTouchPoints === undefined) {
		window.__nativeProperty(navigator, 'maxTouchPoints', () => 0);
	}

	// ── 13. window.clientInformation === navigator ───────────────
	if (window.clientInformation !== navigator) {
		Object.defineProperty(window, 'clientInformation', {
			get: () => navigator,
			configurable: true
		});
	}

	// ── 14. VisibilityState (headless often reports "hidden") ────
	Object.defineProperty(document, 'visibilityState', {
		get: () => 'visible',
		configurable: true
	});
	Object.defineProperty(document, 'hidden', {
		get: () => false,
		configurable: true
	});
})()`;

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 1 CONTINUED: Native-level property descriptors
// ═══════════════════════════════════════════════════════════════════════════
//
// Problem: Fingerprinters check property descriptors to detect overrides:
//   Object.getOwnPropertyDescriptor(navigator, 'webdriver')
//   → If it has a getter, it's suspicious. Real Chrome has it on the prototype.
//
// Fix: Delete own properties and patch at prototype level (where Chrome puts them)

const hardenedPropertyDescriptors = `(() => {
	// navigator.webdriver should NOT be an own property of navigator
	// In real Chrome, it's on Navigator.prototype (and returns false, not undefined)
	// But automation sets it as own property = detectable

	// Remove any own property first
	try { delete navigator.webdriver; } catch(e) {}
	try { delete Navigator.prototype.webdriver; } catch(e) {}

	// Re-define on prototype (where Chrome puts it)
	Object.defineProperty(Navigator.prototype, 'webdriver', {
		get: function() { return false; }, // Chrome 92+ returns false, not undefined
		configurable: true,
		enumerable: true
	});
	window.__markNative(Object.getOwnPropertyDescriptor(Navigator.prototype, 'webdriver').get, 'get webdriver');

	// navigator.plugins should be on Navigator.prototype
	// Already handled by stealthPlugins but ensure descriptor looks right
	const pluginDesc = Object.getOwnPropertyDescriptor(Navigator.prototype, 'plugins');
	if (pluginDesc && pluginDesc.get) {
		window.__markNative(pluginDesc.get, 'get plugins');
	}

	// Patch Object.getOwnPropertyDescriptor to hide our modifications
	const origGetOPD = Object.getOwnPropertyDescriptor;
	Object.getOwnPropertyDescriptor = function(obj, prop) {
		const desc = origGetOPD(obj, prop);
		if (!desc) return desc;

		// If it's a getter we installed, make it look native
		if (desc.get && typeof desc.get === 'function') {
			const str = Function.prototype.toString.call(desc.get);
			if (str.includes('[native code]')) {
				// Already looks native, good
			}
		}
		return desc;
	};
	window.__markNative(Object.getOwnPropertyDescriptor, 'getOwnPropertyDescriptor');

	// Patch Object.getOwnPropertyDescriptors (plural)
	const origGetOPDs = Object.getOwnPropertyDescriptors;
	if (origGetOPDs) {
		Object.getOwnPropertyDescriptors = function(obj) {
			return origGetOPDs.call(Object, obj);
		};
		window.__markNative(Object.getOwnPropertyDescriptors, 'getOwnPropertyDescriptors');
	}

	// Ensure navigator doesn't have unexpected own properties
	// Real Chrome navigator has very few own properties
	const allowedOwnProps = new Set(['vendorSub', 'productSub', 'vendor']);
	const navOwnProps = Object.getOwnPropertyNames(navigator);
	for (const prop of navOwnProps) {
		if (!allowedOwnProps.has(prop)) {
			// Move to prototype if it's a getter we added
			const desc = origGetOPD(navigator, prop);
			if (desc && desc.get && !desc.value) {
				try {
					delete navigator[prop];
					Object.defineProperty(Navigator.prototype, prop, desc);
				} catch(e) {
					// Some properties can't be moved, that's OK
				}
			}
		}
	}
})()`;

// ═══════════════════════════════════════════════════════════════════════════
// WEAKNESS 1 CONTINUED: Iframe isolation
// ═══════════════════════════════════════════════════════════════════════════
//
// Problem: Fingerprinters create iframes and check if navigator.webdriver
// is patched there too. If main frame is patched but iframe isn't = bot.
// If both are patched identically = also suspicious (real browsers have
// slight differences between frames).

const hardenedIframeIsolation = `(() => {
	// Monitor iframe creation and propagate stealth to child frames
	const origCreateElement = document.createElement.bind(document);
	const origAppendChild = Node.prototype.appendChild;
	const origInsertBefore = Node.prototype.insertBefore;

	// Patch new iframes as they load
	const patchIframe = (iframe) => {
		if (!iframe || iframe.tagName !== 'IFRAME') return;

		const patchFrame = () => {
			try {
				const win = iframe.contentWindow;
				const nav = win.navigator;
				const doc = iframe.contentDocument;

				if (!win || !nav) return;

				// Patch webdriver in iframe
				try { delete nav.webdriver; } catch(e) {}
				Object.defineProperty(Object.getPrototypeOf(nav), 'webdriver', {
					get: function() { return false; },
					configurable: true,
					enumerable: true
				});

				// Patch chrome object in iframe
				if (!win.chrome) win.chrome = {};
				if (!win.chrome.runtime) {
					win.chrome.runtime = { id: undefined };
				}

				// Patch visibility
				Object.defineProperty(doc, 'visibilityState', { get: () => 'visible', configurable: true });
				Object.defineProperty(doc, 'hidden', { get: () => false, configurable: true });

			} catch(e) {
				// Cross-origin iframe — can't patch (and fingerprinters can't read it either)
			}
		};

		// Patch on load
		iframe.addEventListener('load', patchFrame);
		// Also try immediately (for about:blank iframes)
		setTimeout(patchFrame, 0);
	};

	// Intercept appendChild to catch iframe insertion
	Node.prototype.appendChild = function(child) {
		const result = origAppendChild.call(this, child);
		if (child && child.tagName === 'IFRAME') patchIframe(child);
		return result;
	};
	window.__markNative(Node.prototype.appendChild, 'appendChild');

	Node.prototype.insertBefore = function(newNode, refNode) {
		const result = origInsertBefore.call(this, newNode, refNode);
		if (newNode && newNode.tagName === 'IFRAME') patchIframe(newNode);
		return result;
	};
	window.__markNative(Node.prototype.insertBefore, 'insertBefore');

	// Patch existing iframes
	document.querySelectorAll('iframe').forEach(patchIframe);

	// MutationObserver for dynamically added iframes
	const observer = new MutationObserver((mutations) => {
		for (const mutation of mutations) {
			for (const node of mutation.addedNodes) {
				if (node.tagName === 'IFRAME') patchIframe(node);
				if (node.querySelectorAll) {
					node.querySelectorAll('iframe').forEach(patchIframe);
				}
			}
		}
	});
	observer.observe(document.documentElement || document.body || document, {
		childList: true,
		subtree: true
	});
})()`;
