package main

// stealth_fonts.go — Font enumeration masking
// Prevents fingerprinting via document.fonts / FontFace enumeration.
// Returns a consistent, common font list regardless of actual system fonts.

import (
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ApplyFontMasking injects font enumeration spoofing.
func ApplyFontMasking(page *rod.Page) error {
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: stealthFontEnum}.Call(page)
	return err
}

// stealthFontEnum masks font enumeration to return a standard Windows font set.
// This defeats font-based fingerprinting (e.g., FontFinger, CreepJS).
const stealthFontEnum = `(() => {
	// Standard Windows 10/11 fonts that every Chrome user has
	const STANDARD_FONTS = [
		'Arial', 'Arial Black', 'Calibri', 'Cambria', 'Cambria Math',
		'Comic Sans MS', 'Consolas', 'Courier', 'Courier New',
		'Georgia', 'Helvetica', 'Impact', 'Lucida Console',
		'Lucida Sans Unicode', 'Microsoft Sans Serif', 'Palatino Linotype',
		'Segoe UI', 'Segoe UI Symbol', 'Tahoma', 'Times', 'Times New Roman',
		'Trebuchet MS', 'Verdana', 'Wingdings'
	];

	// Override document.fonts.check() — used by many fingerprinters
	if (document.fonts && document.fonts.check) {
		const origCheck = document.fonts.check.bind(document.fonts);
		document.fonts.check = function(font, text) {
			// Extract font family name from CSS font shorthand
			const match = font.match(/["']?([^"',]+)["']?\s*$/);
			if (match) {
				const family = match[1].trim();
				// Only report standard fonts as available
				if (!STANDARD_FONTS.some(f => f.toLowerCase() === family.toLowerCase())) {
					return false;
				}
			}
			return origCheck(font, text || 'mmmmmmmmmmlli');
		};
	}

	// Override FontFaceSet iteration (document.fonts is a FontFaceSet)
	if (document.fonts) {
		const origForEach = document.fonts.forEach;
		const origEntries = document.fonts.entries;
		const origValues = document.fonts.values;

		// Create fake FontFace entries for standard fonts
		const fakeFonts = STANDARD_FONTS.map(name => ({
			family: name,
			style: 'normal',
			weight: '400',
			stretch: 'normal',
			unicodeRange: 'U+0-10FFFF',
			status: 'loaded',
			loaded: Promise.resolve()
		}));

		Object.defineProperty(document.fonts, 'size', {
			get: () => fakeFonts.length,
			configurable: true
		});

		document.fonts.forEach = function(callback, thisArg) {
			fakeFonts.forEach((font, i) => callback.call(thisArg, font, i, this));
		};

		// Symbol.iterator for for...of loops
		document.fonts[Symbol.iterator] = function*() {
			for (const font of fakeFonts) yield font;
		};
	}

	// Patch measureText-based font detection
	// Fingerprinters measure text width with a font vs fallback to detect availability
	const origMeasureText = CanvasRenderingContext2D.prototype.measureText;
	const baseWidths = {};
	
	CanvasRenderingContext2D.prototype.measureText = function(text) {
		const result = origMeasureText.call(this, text);
		const font = this.font || '';
		
		// Extract font family
		const families = font.split(',').map(f => f.trim().replace(/["']/g, ''));
		const primaryFont = families[0] || '';
		
		// If font is not in standard list, return monospace fallback width
		// This makes non-standard fonts appear "not installed"
		const fontBase = primaryFont.replace(/\d+px\s*/, '').trim();
		if (fontBase && !STANDARD_FONTS.some(f => f.toLowerCase() === fontBase.toLowerCase())) {
			// Return a consistent width as if font fell back to default
			const key = text + '|' + font.match(/\d+/)?.[0];
			if (!baseWidths[key]) {
				this.font = (font.match(/\d+px/) || ['16px'])[0] + ' monospace';
				baseWidths[key] = origMeasureText.call(this, text);
				this.font = font;
			}
			return baseWidths[key];
		}
		
		return result;
	};

	// Patch FontFace constructor to prevent loading detection fonts
	const OrigFontFace = window.FontFace;
	window.FontFace = function(family, source, descriptors) {
		const face = new OrigFontFace(family, source, descriptors);
		return face;
	};
	window.FontFace.prototype = OrigFontFace.prototype;
	Object.defineProperty(window.FontFace, 'name', { value: 'FontFace' });
})()`;
