import { defineConfig, devices } from "@playwright/test";
import os from "node:os";
import path from "node:path";

const artifactRoot = path.join(os.tmpdir(), `verity-playwright-${process.pid}`);

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:3100",
    trace: "retain-on-failure",
  },
  webServer: [
    {
      command: `cd ../api && VERITY_ARTIFACT_ROOT=${artifactRoot} VERITY_API_ADDR=127.0.0.1:8100 VERITY_ALLOWED_ORIGINS=http://127.0.0.1:3100 go run ./cmd/verity-api`,
      url: "http://127.0.0.1:8100/ready",
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: "until curl -fsS http://127.0.0.1:8100/ready >/dev/null; do sleep 0.1; done; curl -fsS -X POST http://127.0.0.1:8100/api/v1/runs >/dev/null; VERITY_API_URL=http://127.0.0.1:8100 NEXT_PUBLIC_API_URL=http://127.0.0.1:8100 npm run build && VERITY_API_URL=http://127.0.0.1:8100 NEXT_PUBLIC_API_URL=http://127.0.0.1:8100 npm run start -- --hostname 127.0.0.1 --port 3100",
      url: "http://127.0.0.1:3100",
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1600, height: 1000 } } },
  ],
});
