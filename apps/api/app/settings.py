from __future__ import annotations

from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="VERITY_", extra="ignore")

    repo_root: Path = Path(__file__).resolve().parents[3]
    artifact_root: Path | None = None
    sample_root: Path | None = None
    web_fixture_path: Path | None = None

    @property
    def artifacts(self) -> Path:
        return self.artifact_root or self.repo_root / "artifacts"

    @property
    def samples(self) -> Path:
        return self.sample_root or self.repo_root / "sample_data" / "raw"

    @property
    def web_fixture(self) -> Path:
        return (
            self.web_fixture_path
            or self.repo_root / "apps" / "web" / "src" / "data" / "demo-workspace.json"
        )


@lru_cache
def get_settings() -> Settings:
    return Settings()
