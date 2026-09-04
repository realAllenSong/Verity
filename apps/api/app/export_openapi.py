from __future__ import annotations

import json
from pathlib import Path

from .main import app


def main() -> None:
    repo_root = Path(__file__).resolve().parents[3]
    target = repo_root / "packages" / "contracts" / "openapi.json"
    target.write_text(json.dumps(app.openapi(), indent=2, sort_keys=True) + "\n")
    print(target)


if __name__ == "__main__":
    main()
