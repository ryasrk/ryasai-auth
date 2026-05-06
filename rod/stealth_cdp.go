package main

// stealth_cdp.go — CDP leak patches & go-rod/stealth integration
// Covers: go-rod/stealth integration, CDP protocol cloaking (execution context leaks)

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// ApplyRodStealth uses go-rod/stealth package JS to patch known CDP leaks.
// Injects the full puppeteer-extra-stealth evasions via EvalOnNewDocument.
func ApplyRodStealth(page *rod.Page) (*rod.Page, error) {
	// go-rod/stealth.JS contains all puppeteer-extra-stealth evasions:
	// navigator.webdriver, chrome.runtime, plugins, languages, WebGL, etc.
	_, err := page.EvalOnNewDocument(stealth.JS)
	if err != nil {
		return page, err
	}
	return page, nil
}

// --- CDP Protocol Cloaking (Execution Context Leaks) ---

// stealthCDPRuntime hides Runtime.enable artifacts that fingerprinters detect.
// Detects: Runtime.executionContextCreated leak, __cdp_binding__, etc.
const stealthCDPRuntime = `(() => {
	// Remove CDP binding artifacts
	const bindingKeys = Object.keys(window).filter(k => 
		k.startsWith('__cdp') || k.startsWith('__puppeteer') || k.startsWith('__rod')
	);
	bindingKeys.forEach(k => { delete window[k]; });

	// Patch Error.stack to remove DevTools protocol frames
	const origPrepareStackTrace = Error.prepareStackTrace;
	Error.prepareStackTrace = function(error, structuredStackTrace) {
		const filtered = structuredStackTrace.filter(frame => {
			const fn = frame.getFunctionName() || '';
			const file = frame.getFileName() || '';
			return !fn.includes('__cdp') && 
			       !fn.includes('Runtime.evaluate') &&
			       !file.includes('pptr:') &&
			       !file.includes('__puppeteer_evaluation_script__');
		});
		if (origPrepareStackTrace) {
			return origPrepareStackTrace(error, filtered);
		}
		return filtered.map(f => '    at ' + f.toString()).join('\n');
	};
})()`;

// stealthCDPExecContext patches execution context isolation detection.
// Sites check if eval'd code runs in a different context than page code.
const stealthCDPExecContext = `(() => {
	// Ensure document.hasFocus() returns true (CDP pages often return false)
	Object.defineProperty(document, 'hasFocus', {
		value: () => true,
		writable: false,
		configurable: true
	});

	// Patch window.chrome to look like real Chrome
	if (!window.chrome) {
		window.chrome = {};
	}
	if (!window.chrome.runtime) {
		window.chrome.runtime = {
			connect: function() { return { onMessage: { addListener: function(){} }, postMessage: function(){} }; },
			sendMessage: function() {},
			id: undefined
		};
	}
	// Ensure chrome.runtime.id is undefined (not missing) — real Chrome behavior
	Object.defineProperty(window.chrome.runtime, 'id', {
		get: () => undefined,
		configurable: true
	});

	// Patch Reflect.ownKeys to hide injected properties
	const origOwnKeys = Reflect.ownKeys;
	const hiddenKeys = new Set(['__cdp_binding__', '__rod_binding__', 'cdc_adoQpoasnfa76pfcZLmcfl_Array',
		'cdc_adoQpoasnfa76pfcZLmcfl_Promise', 'cdc_adoQpoasnfa76pfcZLmcfl_Symbol']);
	Reflect.ownKeys = function(target) {
		const keys = origOwnKeys(target);
		if (target === window || target === document) {
			return keys.filter(k => !hiddenKeys.has(String(k)));
		}
		return keys;
	};
})()`;

// stealthCDPDebugger prevents debugger detection via CDP
const stealthCDPDebugger = `(() => {
	// Neutralize debugger statement detection
	// Some sites use: (function(){debugger})['constructor']('debugger')()
	const origFunction = Function.prototype.constructor;
	Function.prototype.constructor = function() {
		const args = Array.from(arguments);
		const body = args[args.length - 1] || '';
		if (typeof body === 'string' && body.trim() === 'debugger') {
			return function() {};
		}
		return origFunction.apply(this, args);
	};
})()`;

// ApplyCDPCloaking injects all CDP-related stealth scripts via AddScriptToEvaluateOnNewDocument
func ApplyCDPCloaking(page *rod.Page) error {
	cdpScripts := []string{
		stealthCDPRuntime,
		stealthCDPExecContext,
		stealthCDPDebugger,
	}

	for _, script := range cdpScripts {
		_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
		if err != nil {
			return err
		}
	}
	return nil
}
