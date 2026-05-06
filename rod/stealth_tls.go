package main

// stealth_tls.go — TLS fingerprint randomization (JA3/JA4)
// Randomizes TLS ClientHello to avoid JA3/JA4 fingerprint correlation.
// Works by setting Chrome flags that alter cipher suite ordering and extensions.

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// TLSProfile represents a set of Chrome flags that produce different JA3/JA4 hashes.
type TLSProfile struct {
	Name        string
	CipherFlags []string
	TLSVersion  string
	ECHEnabled  bool
}

// Known TLS profiles that produce different JA3 fingerprints.
// These mimic real Chrome configurations seen in the wild.
var tlsProfiles = []TLSProfile{
	{
		Name: "chrome-default",
		CipherFlags: []string{},
		TLSVersion:  "1.3",
		ECHEnabled:  true,
	},
	{
		Name: "chrome-compat",
		CipherFlags: []string{
			"--cipher-suite-blacklist=0xc02c,0xc02b", // Disable some ECDHE suites
		},
		TLSVersion: "1.3",
		ECHEnabled: false,
	},
	{
		Name: "chrome-legacy-order",
		CipherFlags: []string{
			"--disable-features=PermuteTLSExtensions", // Disable extension permutation
		},
		TLSVersion: "1.3",
		ECHEnabled: true,
	},
	{
		Name: "chrome-no-ech",
		CipherFlags: []string{
			"--disable-features=EncryptedClientHello",
		},
		TLSVersion: "1.3",
		ECHEnabled: false,
	},
	{
		Name: "chrome-permuted",
		CipherFlags: []string{
			"--enable-features=PermuteTLSExtensions", // Force extension permutation
		},
		TLSVersion: "1.3",
		ECHEnabled: true,
	},
	{
		Name: "chrome-kyber",
		CipherFlags: []string{
			"--enable-features=PostQuantumKyber", // Post-quantum key exchange
		},
		TLSVersion: "1.3",
		ECHEnabled: true,
	},
}

// RandomTLSProfile picks a random TLS profile.
func RandomTLSProfile() TLSProfile {
	return tlsProfiles[rand.Intn(len(tlsProfiles))]
}

// ApplyTLSRandomization applies TLS fingerprint randomization flags to the launcher.
// This changes the JA3/JA4 hash by altering cipher suites and TLS extensions.
func ApplyTLSRandomization(l *launcher.Launcher, profile TLSProfile) *launcher.Launcher {
	// Apply cipher-related flags
	for _, flag := range profile.CipherFlags {
		parts := strings.SplitN(flag, "=", 2)
		key := strings.TrimPrefix(parts[0], "--")
		if len(parts) == 2 {
			l = l.Set(flags.Flag(key), parts[1])
		} else {
			l = l.Set(flags.Flag(key), "")
		}
	}

	// GREASE values are randomized internally by Chrome based on session.
	// The TLS extension permutation flag above handles JA3 diversity.

	// Enable/disable ECH (Encrypted Client Hello) — changes JA4 fingerprint
	if !profile.ECHEnabled {
		l = l.Set(flags.Flag("disable-features"), "EncryptedClientHello")
	}

	// Randomize TLS session ticket behavior
	if rand.Intn(2) == 0 {
		l = l.Set(flags.Flag("disable-features"), "TLS13EarlyData")
	}

	return l
}



// GetTLSProfileInfo returns debug info about the selected TLS profile.
func GetTLSProfileInfo(profile TLSProfile) string {
	return fmt.Sprintf("TLS Profile: %s (TLS %s, ECH=%v, flags=%d)",
		profile.Name, profile.TLSVersion, profile.ECHEnabled, len(profile.CipherFlags))
}
