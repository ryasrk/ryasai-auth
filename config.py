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
    concurrency: int = Field(default=2, alias="CONCURRENCY")

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
