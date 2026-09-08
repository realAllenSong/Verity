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

  for (const stage of ["Raw", "Normalize", "Privacy", "Quality", "Extract", "Review", "Ready"]) {
    await page.locator(".pipeline-flow").getByRole("button", { name: new RegExp(stage, "i") }).click();
    await expect(page.locator(".stage-record-table tbody tr").first()).toBeVisible();
    await page.locator(".row-inspect").first().click();
    await expect(page.getByRole("dialog").getByText("One record across this pipeline boundary.")).toBeVisible();
    if (stage === "Privacy") await page.screenshot({ path: "../../output/playwright/verity-privacy-detail.png", animations: "disabled" });
    await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }
  await page.screenshot({ path: "../../output/playwright/verity-workbench-desktop.png", fullPage: true, animations: "disabled" });

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
  await page.screenshot({ path: "../../output/playwright/verity-workbench-mobile.png", fullPage: true });
});

test("review reaches beyond preview samples and updates the downloaded result", async ({ page, request }) => {
  await page.goto("/");
  const review = page.getByRole("button", { name: /Review \d+ items/i });
  const before = Number((await review.innerText()).match(/\d+/)?.[0]);
  expect(before).toBeGreaterThan(10);
  await review.click();
  const dialog = page.getByRole("dialog");
  let acceptedID = "";
  for (let index = 0; index < 10; index++) {
    const card = dialog.locator(".review-card").first();
    const id = await card.locator("code").innerText();
    if (index === 9) acceptedID = id;
    const saved = page.waitForResponse((response) => response.url().includes("/api/v1/reviews/") && response.request().method() === "PATCH");
    await card.getByRole("button", { name: "Accept", exact: true }).click();
    expect((await saved).ok()).toBeTruthy();
    await expect(dialog.locator(".review-card").first().locator("code")).not.toHaveText(id);
  }
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(review).toHaveText(`Review ${before - 10} items`);
  const ws = await (await request.get("http://127.0.0.1:8100/api/v1/workspace")).json();
  const output = ws.outputs.find((item: { format: string; name: string }) => item.format === "jsonl" && item.name === "Ready records");
  const result = await request.get(`http://127.0.0.1:8100/api/v1/outputs/${output.id}`);
  expect(result.ok()).toBeTruthy();
  expect(await result.text()).toContain(`"event_id":"${acceptedID}"`);
});

test("secondary workspace actions open useful details", async ({ page }) => {
  await page.goto("/");
  for (const action of ["Run history", "Workflow details", "Automation"]) {
    await page.getByRole("button", { name: "Workspace options" }).click();
    await page.getByRole("menuitem", { name: action }).click();
    await expect(page.getByRole("dialog").getByRole("heading", { name: action, exact: true })).toBeVisible();
    await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  }
});

test("the entire stage can be browsed beyond its representative sample", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Browse all records" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".browse-record")).toHaveCount(25);
  const first = await dialog.locator(".browse-record summary").first().innerText();
  await dialog.getByRole("button", { name: "Next", exact: true }).click();
  await expect(dialog.locator(".browse-record summary").first()).not.toHaveText(first);
  await dialog.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(dialog.locator(".browse-record summary").first()).toHaveText(first);
  await dialog.locator(".browse-record summary").first().click();
  await expect(dialog.locator(".browse-record[open] dl")).toBeVisible();
});
