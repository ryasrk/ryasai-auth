package main

// stealth_timezone.go — Timezone spoofing (Intl.DateTimeFormat + Chrome flag)

import (
	"fmt"
	"math/rand"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Common timezone/locale pairs that look natural together
type TimezoneProfile struct {
	Timezone string
	Locale   string
	Offset   int // UTC offset in minutes
}

var timezoneProfiles = []TimezoneProfile{
	{"America/New_York", "en-US", -300},
	{"America/Chicago", "en-US", -360},
	{"America/Los_Angeles", "en-US", -480},
	{"America/Denver", "en-US", -420},
	{"Europe/London", "en-GB", 0},
	{"Europe/Berlin", "en-DE", 60},
	{"Europe/Paris", "fr-FR", 60},
	{"Asia/Tokyo", "ja-JP", 540},
	{"Asia/Singapore", "en-SG", 480},
	{"Australia/Sydney", "en-AU", 660},
}

// RandomTimezoneProfile picks a random timezone profile.
// If proxy geo is known, caller should use MatchTimezoneToGeo instead.
func RandomTimezoneProfile() TimezoneProfile {
	return timezoneProfiles[rand.Intn(len(timezoneProfiles))]
}

// MatchTimezoneToGeo returns a timezone profile matching a country code.
func MatchTimezoneToGeo(countryCode string) TimezoneProfile {
	geoMap := map[string]TimezoneProfile{
		"US": {"America/New_York", "en-US", -300},
		"GB": {"Europe/London", "en-GB", 0},
		"DE": {"Europe/Berlin", "en-DE", 60},
		"FR": {"Europe/Paris", "fr-FR", 60},
		"JP": {"Asia/Tokyo", "ja-JP", 540},
		"SG": {"Asia/Singapore", "en-SG", 480},
		"AU": {"Australia/Sydney", "en-AU", 660},
	}
	if tz, ok := geoMap[countryCode]; ok {
		return tz
	}
	// Default to US East
	return timezoneProfiles[0]
}

// ApplyTimezoneFlag adds --tz Chrome flag at launcher level.
func ApplyTimezoneFlag(l *launcher.Launcher, tz TimezoneProfile) *launcher.Launcher {
	// Chrome respects TZ env var passed via --timezone flag
	l = l.Env("TZ=" + tz.Timezone)
	return l
}

// ApplyTimezoneSpoof injects JS to override Intl.DateTimeFormat and Date methods.
func ApplyTimezoneSpoof(page *rod.Page, tz TimezoneProfile) error {
	script := fmt.Sprintf(stealthTimezoneTemplate, tz.Timezone, tz.Offset, tz.Locale)
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
	return err
}

// stealthTimezoneTemplate — %s = timezone name, %d = offset minutes, %s = locale
const stealthTimezoneTemplate = `(() => {
	const TIMEZONE = '%s';
	const OFFSET = %d;
	const LOCALE = '%s';

	// Override Intl.DateTimeFormat
	const OrigDateTimeFormat = Intl.DateTimeFormat;
	const handler = {
		construct(target, args) {
			const opts = args[1] || {};
			if (!opts.timeZone) {
				opts.timeZone = TIMEZONE;
			}
			args[1] = opts;
			if (!args[0]) args[0] = LOCALE;
			return new OrigDateTimeFormat(...args);
		},
		apply(target, thisArg, args) {
			const opts = args[1] || {};
			if (!opts.timeZone) {
				opts.timeZone = TIMEZONE;
			}
			args[1] = opts;
			if (!args[0]) args[0] = LOCALE;
			return OrigDateTimeFormat(...args);
		}
	};
	window.Intl.DateTimeFormat = new Proxy(OrigDateTimeFormat, handler);
	// Preserve prototype chain
	window.Intl.DateTimeFormat.prototype = OrigDateTimeFormat.prototype;
	Object.defineProperty(window.Intl.DateTimeFormat, 'name', { value: 'DateTimeFormat' });

	// Override resolvedOptions to return our timezone
	const origResolvedOptions = OrigDateTimeFormat.prototype.resolvedOptions;
	OrigDateTimeFormat.prototype.resolvedOptions = function() {
		const result = origResolvedOptions.call(this);
		result.timeZone = TIMEZONE;
		result.locale = LOCALE;
		return result;
	};

	// Override Date.prototype.getTimezoneOffset
	Date.prototype.getTimezoneOffset = function() {
		return OFFSET * -1; // JS returns inverted offset
	};

	// Override Date.prototype.toString and toTimeString
	const origToString = Date.prototype.toString;
	Date.prototype.toString = function() {
		const str = origToString.call(this);
		// Replace timezone abbreviation
		return str.replace(/\(.*\)/, '(' + TIMEZONE.split('/').pop().replace('_', ' ') + ')');
	};

	const origToLocaleString = Date.prototype.toLocaleString;
	Date.prototype.toLocaleString = function(locale, opts) {
		opts = opts || {};
		if (!opts.timeZone) opts.timeZone = TIMEZONE;
		return origToLocaleString.call(this, locale || LOCALE, opts);
	};
})()`;
