"""ryasai-auth configuration."""

from __future__ import annotations

import secrets
from functools import lru_cache

from pydantic import Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class AuthSettings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # ── Server Connection ────────────────────────────────────────
    server_url: str = Field(
        default="http://localhost:3456",
        alias="SERVER_URL",
    )
    server_admin_key: str = Field(
        default="",
        alias="SERVER_ADMIN_KEY",
    )

    # ── Providers ────────────────────────────────────────────────
    providers: str = Field(
        default="kiro,codebuddy",
        alias="PROVIDERS",
    )

    # ── Worker ───────────────────────────────────────────────────
    worker_id: str = Field(default="", alias="WORKER_ID")
    concurrency: int = Field(default=1, alias="CONCURRENCY")
    # True = run N accounts in parallel; False = sequential (1 at a time)
    parallel: bool = Field(default=True, alias="PARALLEL")

    # ── Anti-Ban / Isolation ─────────────────────────────────────
    # Delay (seconds) between finishing one account and starting the next
    account_delay: float = Field(default=5.0, alias="ACCOUNT_DELAY")
    # Random jitter added to delay (0 to this value)
    account_delay_jitter: float = Field(default=3.0, alias="ACCOUNT_DELAY_JITTER")

    # ── Browser ──────────────────────────────────────────────────
    camoufox_headless: str = Field(default="true", alias="CAMOUFOX_HEADLESS")
    proxy_url: str = Field(default="", alias="PROXY_URL")

    # ── Logging ──────────────────────────────────────────────────
    log_level: str = Field(default="INFO", alias="LOG_LEVEL")

    @model_validator(mode="after")
    def _defaults(self) -> "AuthSettings":
        if not self.worker_id:
            self.worker_id = f"ryasai-auth-{secrets.token_hex(4)}"
        return self

    @property
    def provider_list(self) -> list[str]:
        return [p.strip().lower() for p in self.providers.split(",") if p.strip()]


@lru_cache(maxsize=1)
def get_settings() -> AuthSettings:
    return AuthSettings()
