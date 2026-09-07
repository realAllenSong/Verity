import path from "node:path";
import { expect, test } from "@playwright/test";

const fixture = path.resolve(process.cwd(), "../../sample_data/examples/noisy-workflow-events.csv");

test("the workspace has one obvious upload-first entry point", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /Drop data or choose files/i })).toBeVisible();
  await expect(page.locator(".left-rail")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Run pipeline/i })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Stage batch/i })).toHaveCount(0);
});

test("dropping a file starts the same automatic flow", async ({ page }) => {
  await page.goto("/");
  const dropzone = page.locator('.import-surface[data-ready="true"]');
  await expect(dropzone).toBeVisible();
  await dropzone.evaluate((element) => {
    const transfer = new DataTransfer();
    transfer.items.add(new File([
      "record_id,kind,occurred_at,title,content\n",
      "drop_1,agent_steer,2026-09-06T12:00:00Z,Steer,Developer steered the coding agent toward the repository adapter pattern.\n",
    ], "dropped-workflow.csv", { type: "text/csv", lastModified: 1_788_739_200_000 }));
    element.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: transfer }));
  });
  await expect(page.getByText("dropped-workflow.csv", { exact: true })).toBeVisible();
  await expect(page.locator(".job-state strong").getByText("Result ready", { exact: true })).toBeVisible({ timeout: 60_000 });
});

test("a CSV upload automatically becomes an inspectable downloadable result", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(message.text()); });
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  await page.goto("/");

  const chooserPromise = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: /Drop data or choose files/i }).click();
  const chooser = await chooserPromise;
  await chooser.setFiles(fixture);
  await expect(page.getByText("noisy-workflow-events.csv", { exact: true })).toBeVisible();
  await expect(page.locator(".job-state strong").getByText("Result ready", { exact: true })).toBeVisible({ timeout: 60_000 });
  await expect(page.getByRole("button", { name: /Raw/i })).toBeVisible();
  await expect(page.getByRole("button", { name: /Privacy/i })).toBeVisible();

  await page.getByRole("button", { name: /Privacy/i }).click();
  await expect(page.getByRole("heading", { name: "Privacy", exact: true })).toBeVisible();
  await expect(page.getByText("Before", { exact: true }).or(page.getByText("Representative records", { exact: false }))).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("link", { name: /Download result/i }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/curated\.(parquet|csv|jsonl)/);
  expect(consoleErrors).toEqual([]);
});

test("review is available as a contextual task, not a navigation section", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: /Review \d+ items/i }).click();
  await expect(page.getByRole("dialog").getByRole("heading", { name: "Review uncertain records" })).toBeVisible();
  await expect(page.getByRole("dialog").getByRole("button", { name: "Accept" }).first()).toBeVisible();
});

test("mobile keeps upload, pipeline, and contextual actions readable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("button", { name: /Drop data or choose files/i })).toBeVisible();
  await expect(page.getByRole("button", { name: /Review \d+ items/i })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
});
