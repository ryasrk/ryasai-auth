package main

// stealth_connection.go — navigator.connection spoofing
// Spoofs Network Information API to report consistent connection type.

import (
	"fmt"
	"math/rand"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ConnectionProfile represents a realistic network connection profile.
type ConnectionProfile struct {
	EffectiveType string  // "4g", "3g", "2g"
	Downlink      float64 // Mbps
	RTT           int     // milliseconds
	SaveData      bool
	Type          string  // "wifi", "cellular", "ethernet"
}

var connectionProfiles = []ConnectionProfile{
	{"4g", 10.0, 50, false, "wifi"},
	{"4g", 8.5, 75, false, "wifi"},
	{"4g", 15.0, 40, false, "ethernet"},
	{"4g", 5.6, 100, false, "wifi"},
	{"4g", 20.0, 30, false, "ethernet"},
	{"4g", 7.2, 60, false, "wifi"},
}

// RandomConnectionProfile picks a realistic connection profile.
func RandomConnectionProfile() ConnectionProfile {
	return connectionProfiles[rand.Intn(len(connectionProfiles))]
}

// ApplyConnectionSpoof injects navigator.connection spoofing.
func ApplyConnectionSpoof(page *rod.Page, cp ConnectionProfile) error {
	script := fmt.Sprintf(stealthConnectionTemplate,
		cp.EffectiveType, cp.Downlink, cp.RTT, cp.SaveData, cp.Type)
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
	return err
}

// stealthConnectionTemplate — %s=effectiveType, %f=downlink, %d=rtt, %t=saveData, %s=type
const stealthConnectionTemplate = `(() => {
	const EFFECTIVE_TYPE = '%s';
	const DOWNLINK = %f;
	const RTT = %d;
	const SAVE_DATA = %t;
	const TYPE = '%s';

	// Build a fake NetworkInformation object
	const connectionInfo = {
		effectiveType: EFFECTIVE_TYPE,
		downlink: DOWNLINK,
		rtt: RTT,
		saveData: SAVE_DATA,
		type: TYPE,
		downlinkMax: Infinity,
		// Event handlers
		onchange: null,
		addEventListener: function(type, listener) {},
		removeEventListener: function(type, listener) {},
		dispatchEvent: function(event) { return true; }
	};

	// Set prototype to NetworkInformation if it exists
	if (window.NetworkInformation) {
		Object.setPrototypeOf(connectionInfo, NetworkInformation.prototype);
	}

	// Override navigator.connection
	Object.defineProperty(navigator, 'connection', {
		get: () => connectionInfo,
		configurable: true,
		enumerable: true
	});

	// Also handle mozConnection and webkitConnection
	Object.defineProperty(navigator, 'mozConnection', {
		get: () => undefined,
		configurable: true
	});
	Object.defineProperty(navigator, 'webkitConnection', {
		get: () => connectionInfo,
		configurable: true
	});

	// Ensure onLine is true
	Object.defineProperty(navigator, 'onLine', {
		get: () => true,
		configurable: true
	});
})()`;
