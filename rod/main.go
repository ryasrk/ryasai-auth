package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// LoginRequest is the JSON contract from Python.
type LoginRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	Provider  string `json:"provider"`
	TargetURL string `json:"target_url"`
	Headless  bool   `json:"headless"`
	Proxy     string `json:"proxy"`
}

// LoginResult is the JSON response back to Python.
type LoginResult struct {
	Status  string            `json:"status"`
	Cookies map[string]string `json:"cookies"`
	Error   string            `json:"error"`
}

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError("failed to read stdin: " + err.Error())
		return
	}

	var req LoginRequest
	if err := json.Unmarshal(data, &req); err != nil {
		writeError("invalid JSON input: " + err.Error())
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError("email and password are required")
		return
	}

	browser, err := launchBrowser(req.Headless, req.Proxy)
	if err != nil {
		writeError("browser launch failed: " + err.Error())
		return
	}
	defer browser.MustClose()

	var result LoginResult

	switch {
	case req.Provider == "wavespeed" || req.Provider == "kiro" || strings.Contains(req.TargetURL, "accounts.google.com"):
		result = googleOAuthLogin(browser, req)
	default:
		result = googleOAuthLogin(browser, req)
	}

	writeResult(result)
}

func launchBrowser(headless bool, proxy string) (*rod.Browser, error) {
	l := launcher.New()

	if headless {
		l = l.Headless(true)
	} else {
		l = l.Headless(false)
	}

	if proxy != "" {
		l = l.Proxy(proxy)
	}

	// Apply stealth launcher flags (layers 1-6)
	l = stealthLauncherFlags(l)

	// Apply TLS fingerprint randomization (JA3/JA4)
	tlsProfile := RandomTLSProfile()
	l = ApplyTLSRandomization(l, tlsProfile)

	// Apply timezone at launcher level
	tzProfile := RandomTimezoneProfile()
	l = ApplyTimezoneFlag(l, tzProfile)

	// GPU offload — move rendering/compositing off CPU → GPU
	gpuMode := ResolveGPUMode()
	l = ApplyGPUOffload(l, gpuMode)
	if gpuMode != GPUModeOff {
		l = ApplyGPUCrypto(l)
		fmt.Fprintf(os.Stderr, "[gpu] %s\n", PrintGPUStatus(gpuMode))
	}

	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launcher: %w", err)
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return browser, nil
}

func writeResult(r LoginResult) {
	data, _ := json.Marshal(r)
	fmt.Println(string(data))
}

func writeError(msg string) {
	writeResult(LoginResult{
		Status:  "failed",
		Cookies: map[string]string{},
		Error:   msg,
	})
}
