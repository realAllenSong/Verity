FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app
COPY apps/api/pyproject.toml apps/api/uv.lock ./
RUN uv sync --frozen --no-dev

COPY apps/api/app ./app
ENV PATH="/app/.venv/bin:$PATH" \
    VERITY_REPO_ROOT=/workspace \
    VERITY_WEB_FIXTURE_PATH=/tmp/demo-workspace.json

EXPOSE 8000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
