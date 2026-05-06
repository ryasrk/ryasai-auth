"""Adaptive concurrency throttle for ryasai-auth.

Monitors CPU and memory pressure in real-time and dynamically adjusts
the concurrency semaphore to prevent system overload during parallel
browser logins.

Usage:
    throttle = AdaptiveThrottle(max_concurrent=8)
    async with throttle.acquire():
        await run_login(...)

Features:
  • Auto-scales concurrency down when CPU > threshold
  • Auto-scales back up when pressure drops
  • Memory pressure detection (OOM prevention)
  • Per-process CPU affinity (optional, Linux only)
  • Stagger delay to avoid thundering herd on startup
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
from contextlib import asynccontextmanager
from dataclasses import dataclass, field

logger = logging.getLogger("ryasai-auth.throttle")

try:
    import psutil
    HAS_PSUTIL = True
except ImportError:
    psutil = None  # type: ignore[assignment]
    HAS_PSUTIL = False


@dataclass
class ThrottleConfig:
    """Tunable knobs for adaptive throttling."""

    # Maximum concurrent browser instances (hard cap)
    max_concurrent: int = 8

    # Minimum concurrent (never go below this)
    min_concurrent: int = 1

    # CPU threshold (0-100) — start throttling above this
    cpu_high_watermark: float = 80.0

    # CPU threshold — scale back up below this
    cpu_low_watermark: float = 50.0

    # Memory threshold (%) — hard throttle if above this
    mem_high_watermark: float = 85.0

    # How often to check system metrics (seconds)
    poll_interval: float = 3.0

    # Stagger delay between launching concurrent browsers (seconds)
    # Prevents all N browsers from starting simultaneously
    stagger_delay: float = 1.5

    # Cooldown after scaling down before allowing scale-up (seconds)
    scale_down_cooldown: float = 15.0

    # How many slots to add/remove per adjustment
    scale_step: int = 1


class AdaptiveThrottle:
    """Dynamically adjusts concurrency based on system load.

    Instead of a fixed asyncio.Semaphore(N), this monitors CPU/RAM
    and shrinks/grows the effective concurrency window.
    """

    def __init__(self, config: ThrottleConfig | None = None):
        self.config = config or ThrottleConfig()
        self._current_limit = self.config.max_concurrent
        self._semaphore = asyncio.Semaphore(self._current_limit)
        self._active_count = 0
        self._monitor_task: asyncio.Task | None = None
        self._last_scale_down: float = 0
        self._lock = asyncio.Lock()
        self._stagger_lock = asyncio.Lock()
        self._last_acquire_time: float = 0
        self._running = True

        # Stats
        self._total_acquired = 0
        self._total_throttled = 0
        self._peak_active = 0

    @property
    def current_limit(self) -> int:
        return self._current_limit

    @property
    def active_count(self) -> int:
        return self._active_count

    @property
    def stats(self) -> dict:
        return {
            "current_limit": self._current_limit,
            "active": self._active_count,
            "peak_active": self._peak_active,
            "total_acquired": self._total_acquired,
            "total_throttled": self._total_throttled,
        }

    def start_monitor(self):
        """Start background CPU/memory monitoring task."""
        if not HAS_PSUTIL:
            logger.warning(
                "psutil not installed — adaptive throttling disabled. "
                "Install: pip install psutil"
            )
            return
        if self._monitor_task is None:
            self._monitor_task = asyncio.create_task(self._monitor_loop())

    def stop(self):
        """Stop the monitor."""
        self._running = False
        if self._monitor_task:
            self._monitor_task.cancel()
            self._monitor_task = None

    @asynccontextmanager
    async def acquire(self):
        """Acquire a slot, respecting adaptive limits + stagger delay."""
        # Stagger: don't let all browsers launch at once
        async with self._stagger_lock:
            now = time.monotonic()
            elapsed = now - self._last_acquire_time
            if elapsed < self.config.stagger_delay and self._active_count > 0:
                await asyncio.sleep(self.config.stagger_delay - elapsed)
            self._last_acquire_time = time.monotonic()

        await self._semaphore.acquire()
        self._active_count += 1
        self._total_acquired += 1
        self._peak_active = max(self._peak_active, self._active_count)

        try:
            yield
        finally:
            self._active_count -= 1
            self._semaphore.release()

    async def _monitor_loop(self):
        """Periodically check system metrics and adjust concurrency."""
        while self._running:
            try:
                await asyncio.sleep(self.config.poll_interval)
                await self._check_and_adjust()
            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.debug("Throttle monitor error: %s", e)

    async def _check_and_adjust(self):
        """Read CPU/memory and scale concurrency accordingly."""
        if not HAS_PSUTIL:
            return

        cpu_percent = psutil.cpu_percent(interval=0.5)
        mem = psutil.virtual_memory()
        mem_percent = mem.percent

        # Hard throttle: memory critical
        if mem_percent > self.config.mem_high_watermark:
            await self._scale_down(reason=f"memory={mem_percent:.0f}%")
            return

        # CPU too high: scale down
        if cpu_percent > self.config.cpu_high_watermark:
            await self._scale_down(reason=f"cpu={cpu_percent:.0f}%")
            return

        # CPU low enough: try scale up
        if cpu_percent < self.config.cpu_low_watermark:
            await self._scale_up(reason=f"cpu={cpu_percent:.0f}%")

    async def _scale_down(self, reason: str):
        """Reduce effective concurrency."""
        async with self._lock:
            new_limit = max(
                self.config.min_concurrent,
                self._current_limit - self.config.scale_step,
            )
            if new_limit < self._current_limit:
                old = self._current_limit
                self._current_limit = new_limit
                self._last_scale_down = time.monotonic()
                self._total_throttled += 1

                # Drain one permit from semaphore (blocks next acquire)
                # We don't forcefully kill running tasks — just prevent new ones
                try:
                    self._semaphore._value = max(0, self._semaphore._value - self.config.scale_step)
                except AttributeError:
                    pass  # CPython internal, fallback is fine

                logger.info(
                    "⚡ Throttle DOWN: %d → %d (%s)",
                    old, new_limit, reason,
                )

    async def _scale_up(self, reason: str):
        """Increase effective concurrency if cooldown passed."""
        async with self._lock:
            # Respect cooldown
            elapsed = time.monotonic() - self._last_scale_down
            if elapsed < self.config.scale_down_cooldown:
                return

            new_limit = min(
                self.config.max_concurrent,
                self._current_limit + self.config.scale_step,
            )
            if new_limit > self._current_limit:
                old = self._current_limit
                self._current_limit = new_limit

                # Release additional permits
                for _ in range(self.config.scale_step):
                    self._semaphore.release()

                logger.info(
                    "⚡ Throttle UP: %d → %d (%s)",
                    old, new_limit, reason,
                )


def get_cpu_count() -> int:
    """Get usable CPU count (respects cgroups/containers)."""
    # In Docker/K8s, os.cpu_count() may report host CPUs
    # Check cgroup limits first
    try:
        with open("/sys/fs/cgroup/cpu/cpu.cfs_quota_us") as f:
            quota = int(f.read().strip())
        with open("/sys/fs/cgroup/cpu/cpu.cfs_period_us") as f:
            period = int(f.read().strip())
        if quota > 0:
            return max(1, quota // period)
    except (FileNotFoundError, ValueError):
        pass

    # cgroup v2
    try:
        with open("/sys/fs/cgroup/cpu.max") as f:
            parts = f.read().strip().split()
            if parts[0] != "max":
                quota = int(parts[0])
                period = int(parts[1])
                return max(1, quota // period)
    except (FileNotFoundError, ValueError, IndexError):
        pass

    return os.cpu_count() or 2


def recommend_concurrency() -> int:
    """Recommend max concurrency based on available resources.

    Rule of thumb:
      - Each Chromium browser ≈ 300-500MB RAM + 0.5-1 CPU core during page load
      - Safe default: min(cpu_cores, available_ram_gb // 0.5, 8)
    """
    cpus = get_cpu_count()

    if HAS_PSUTIL:
        mem = psutil.virtual_memory()
        available_gb = mem.available / (1024 ** 3)
        # Each browser needs ~500MB
        mem_slots = int(available_gb / 0.5)
    else:
        # Assume 4GB available if we can't check
        mem_slots = 8

    recommended = min(cpus, mem_slots, 8)
    return max(1, recommended)


def set_process_priority(nice_value: int = 10):
    """Lower process priority to avoid starving system.

    Only effective on Linux/macOS. Ignored on Windows.
    """
    try:
        os.nice(nice_value)
        logger.debug("Process nice set to %d", nice_value)
    except (OSError, AttributeError):
        pass


def pin_to_cpus(cpu_list: list[int] | None = None):
    """Pin process to specific CPU cores (Linux only).

    Useful when running alongside other services — keeps browser
    processes from stealing all cores.
    """
    if not HAS_PSUTIL:
        return

    try:
        p = psutil.Process()
        if cpu_list:
            p.cpu_affinity(cpu_list)
            logger.debug("Pinned to CPUs: %s", cpu_list)
        else:
            # Use half the cores
            all_cpus = list(range(os.cpu_count() or 2))
            half = all_cpus[:len(all_cpus) // 2] or [0]
            p.cpu_affinity(half)
            logger.debug("Pinned to CPUs: %s (half)", half)
    except (AttributeError, OSError) as e:
        logger.debug("CPU pinning not available: %s", e)
