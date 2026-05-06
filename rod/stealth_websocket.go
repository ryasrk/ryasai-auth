package main

// stealth_websocket.go — WebSocket frame timing normalization
// Normalizes WebSocket frame timing to prevent timing-based fingerprinting.
// Sites can fingerprint automation by detecting unnaturally fast/consistent WS frame intervals.

import (
	"fmt"
	"math/rand"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// WebSocketTimingConfig controls the timing normalization parameters.
type WebSocketTimingConfig struct {
	MinDelayMs   int // Minimum added delay per frame
	MaxDelayMs   int // Maximum added delay per frame
	JitterMs     int // Random jitter range
	BatchSize    int // Frames before adding extra pause
	BatchPauseMs int // Extra pause after batch
}

// DefaultWSTimingConfig returns sensible defaults that mimic human browsing.
func DefaultWSTimingConfig() WebSocketTimingConfig {
	return WebSocketTimingConfig{
		MinDelayMs:   2,
		MaxDelayMs:   15,
		JitterMs:     8,
		BatchSize:    5 + rand.Intn(10),
		BatchPauseMs: 20 + rand.Intn(50),
	}
}

// ApplyWebSocketTimingNormalization injects WS frame timing normalization.
func ApplyWebSocketTimingNormalization(page *rod.Page, cfg WebSocketTimingConfig) error {
	script := fmt.Sprintf(stealthWebSocketTemplate,
		cfg.MinDelayMs, cfg.MaxDelayMs, cfg.JitterMs, cfg.BatchSize, cfg.BatchPauseMs)
	_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: script}.Call(page)
	return err
}

// stealthWebSocketTemplate normalizes WebSocket frame timing.
// %d = minDelay, %d = maxDelay, %d = jitter, %d = batchSize, %d = batchPause
const stealthWebSocketTemplate = `(() => {
	const MIN_DELAY = %d;
	const MAX_DELAY = %d;
	const JITTER = %d;
	const BATCH_SIZE = %d;
	const BATCH_PAUSE = %d;

	const OrigWebSocket = window.WebSocket;
	
	class StealthWebSocket extends OrigWebSocket {
		constructor(url, protocols) {
			super(url, protocols);
			this._frameCount = 0;
			this._sendQueue = [];
			this._processing = false;
		}

		send(data) {
			this._frameCount++;
			
			// Add timing normalization to outgoing frames
			const delay = MIN_DELAY + Math.random() * (MAX_DELAY - MIN_DELAY);
			const jitter = (Math.random() - 0.5) * JITTER;
			let totalDelay = Math.max(0, delay + jitter);
			
			// Add batch pause every N frames
			if (this._frameCount %% BATCH_SIZE === 0) {
				totalDelay += Math.random() * BATCH_PAUSE;
			}

			if (totalDelay > 0) {
				this._sendQueue.push({ data, delay: totalDelay });
				this._processQueue();
			} else {
				super.send(data);
			}
		}

		_processQueue() {
			if (this._processing || this._sendQueue.length === 0) return;
			this._processing = true;

			const item = this._sendQueue.shift();
			setTimeout(() => {
				if (this.readyState === WebSocket.OPEN) {
					OrigWebSocket.prototype.send.call(this, item.data);
				}
				this._processing = false;
				this._processQueue();
			}, item.delay);
		}
	}

	// Preserve WebSocket constants and prototype
	StealthWebSocket.CONNECTING = OrigWebSocket.CONNECTING;
	StealthWebSocket.OPEN = OrigWebSocket.OPEN;
	StealthWebSocket.CLOSING = OrigWebSocket.CLOSING;
	StealthWebSocket.CLOSED = OrigWebSocket.CLOSED;

	// Also normalize incoming message timing
	const origAddEventListener = EventTarget.prototype.addEventListener;
	EventTarget.prototype.addEventListener = function(type, listener, options) {
		if (this instanceof OrigWebSocket && type === 'message') {
			const wrappedListener = function(event) {
				// Add small random delay to message processing
				const msgDelay = Math.random() * JITTER;
				if (msgDelay > 1) {
					setTimeout(() => listener.call(this, event), msgDelay);
				} else {
					listener.call(this, event);
				}
			};
			return origAddEventListener.call(this, type, wrappedListener, options);
		}
		return origAddEventListener.call(this, type, listener, options);
	};

	// Replace global WebSocket
	Object.defineProperty(window, 'WebSocket', {
		value: StealthWebSocket,
		writable: true,
		configurable: true
	});

	// Ensure instanceof checks still work
	Object.defineProperty(StealthWebSocket, Symbol.hasInstance, {
		value: (instance) => instance instanceof OrigWebSocket
	});

	// Preserve toString behavior
	Object.defineProperty(StealthWebSocket, 'name', { value: 'WebSocket' });
	StealthWebSocket.prototype.constructor = StealthWebSocket;
})()`;
