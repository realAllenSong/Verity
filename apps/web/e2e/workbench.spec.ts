import path from "node:path";
import os from "node:os";
import { expect, test, type Page } from "@playwright/test";

const fixture = path.resolve(process.cwd(), "../../sample_data/examples/noisy-workflow-events.csv");
const shot = (name: string) => path.join(os.tmpdir(), `verity-grid-${name}.png`);
const dataRows = (page: Page) => page.locator(".data-sheet tbody tr[data-record-id]");
async function openTable(page: Page) {
  if (await page.locator(".data-sheet").count() === 0) await page.getByRole("button", { name: "Table", exact: true }).click();
  await expect(page.locator(".data-sheet")).toBeVisible();
}
async function stage(page: Page, name: string) {
  await page.locator(".pipeline-flow").getByRole("button", { name: new RegExp(name, "i") }).click();
  await expect(page.locator(".text-reader, .data-sheet").first()).toBeVisible();
  await openTable(page);
  await expect(page.getByRole("table", { name: `${name} data table` })).toBeVisible();
  await expect(page.locator(".transformation-table").first()).toHaveAttribute("data-phase", "settled");
}

test("one upload-first entry, without extra navigation", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /Drop data or choose files/i })).toBeVisible();
  await expect(page.locator(".left-rail")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Run pipeline|Stage batch/i })).toHaveCount(0);
  await expect(page.locator(".text-reader")).toBeVisible();
  await expect(page.locator(".text-record")).toHaveCount(100);
  await expect(page.getByText("Readable evidence", { exact: true })).toBeVisible();
});

test("text sources open as a readable stream with inline word changes", async ({ page }) => {
  await page.goto("/");
  await page.locator(".pipeline-flow").getByRole("button", { name: /Normalize/ }).click();
  await expect(page.locator(".text-reader")).toBeVisible();
  await expect(page.locator(".text-record").first()).toBeVisible();
  await expect(page.locator('.text-field mark[data-diff="added"], .text-field mark[data-diff="removed"]').first()).toBeVisible();
  await expect(page.getByText("Changes are marked in the sentence", { exact: false })).toBeVisible();
  await page.screenshot({ path: shot("reader-desktop"), fullPage: true, animations: "disabled" });
  await page.screenshot({ path: shot("reader-viewport"), animations: "disabled" });
  await page.locator(".text-inspect").first().click();
  await expect(page.getByRole("dialog").locator("pre").first()).not.toBeEmpty();
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await expect(page.getByRole("table", { name: "Normalize data table" })).toBeVisible();
});

test("dropping data still runs the pipeline automatically", async ({ page }) => {
  await page.goto("/");
  const dropzone = page.locator('.import-surface[data-ready="true"]');
  await expect(dropzone).toBeVisible();
  await dropzone.evaluate(element => {
    const transfer = new DataTransfer();
    transfer.items.add(new File(["record_id,kind,occurred_at,title,content\ndrop_1,agent_steer,2026-09-06T12:00:00Z,Steer,Developer steered the coding agent toward the repository adapter pattern.\n"], "dropped-workflow.csv", { type: "text/csv", lastModified: 1_788_739_200_000 }));
    element.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: transfer }));
  });
  await expect(page.getByText("dropped-workflow.csv", { exact: true })).toBeVisible();
  await expect(page.locator(".job-state strong")).toHaveText("Result ready", { timeout: 60_000 });
  await openTable(page);
  await expect(dataRows(page)).toHaveCount(100);
});

test("10k noisy CSV to real step diffs and a downloadable result", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  await page.goto("/");
  const chooserPromise = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: /Drop data or choose files/i }).click();
  await (await chooserPromise).setFiles(fixture);
  await expect(page.locator(".job-state strong")).toHaveText("Result ready", { timeout: 60_000 });
  await openTable(page);
  await expect(dataRows(page)).toHaveCount(100);
  await page.screenshot({ path: shot("raw-desktop"), fullPage: true, animations: "disabled" });
  for (const name of ["Raw", "Normalize", "Privacy", "Quality", "Extract", "Review", "Ready"]) {
    await stage(page, name);
    await expect(dataRows(page).first()).toBeVisible();
    await page.locator(".sheet-record-button").first().click();
    await expect(page.getByRole("dialog").locator("pre").first()).not.toBeEmpty();
    await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  }
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("link", { name: /Download result/i }).click();
  expect((await downloadPromise).suggestedFilename()).toMatch(/curated\.(parquet|csv|jsonl)/);
  expect(errors).toEqual([]);
});

test("cell changes are visible without opening one dialog per row", async ({ page }) => {
  await page.goto("/");
  await stage(page, "Normalize");
  await page.getByRole("button", { name: /^Modified \d/ }).click();
  await page.getByRole("button", { name: "Choose columns" }).click();
  await page.getByRole("button", { name: "Show all", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(page.locator('td[data-cell-change="removed"]').first()).toBeVisible();
  await page.locator('td[data-cell-change="added"]').first().scrollIntoViewIfNeeded();
  await expect(page.locator('td[data-cell-change="added"]').first()).toBeVisible();
  await stage(page, "Privacy");
  await page.getByRole("button", { name: /^Modified \d/ }).click();
  await expect(page.locator('td[data-cell-change="changed"]').first()).toBeVisible();
  const cell = page.locator('td[data-cell-change="changed"] .sheet-cell').first();
  await expect(cell.locator("del")).not.toBeEmpty();
  await expect(cell.locator("ins")).not.toBeEmpty();
  await cell.click();
  await expect(page.getByLabel("Selected cell details").locator("pre")).toHaveCount(2);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.getByRole("button", { name: "Close cell details" }).click();
  await page.screenshot({ path: shot("privacy-changes"), fullPage: true, animations: "disabled" });
  await page.getByRole("button", { name: /^Removed \d/ }).click();
  await expect(dataRows(page).first()).toHaveAttribute("data-change", "removed");
  await expect(dataRows(page).first().locator("del").first()).toBeVisible();
  await page.getByRole("button", { name: "After", exact: true }).click();
  await expect(dataRows(page)).toHaveCount(0);
  await expect(page.getByText("These rows have no After values.")).toBeVisible();
  await page.getByRole("button", { name: "Changes", exact: true }).click();
  await expect(dataRows(page).first()).toBeVisible();
});

test("complete pagination, global search, columns, wrapping and page-local groups", async ({ page }) => {
  await page.goto("/");
  await openTable(page);
  await expect(dataRows(page)).toHaveCount(100);
  const first = await dataRows(page).first().getAttribute("data-record-id");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(dataRows(page).first().locator(".sheet-number")).toHaveText("101");
  await page.getByRole("button", { name: "Previous", exact: true }).click();
  await expect(dataRows(page).first()).toHaveAttribute("data-record-id", first!);
  await page.getByRole("button", { name: "Choose columns" }).click();
  await page.getByRole("button", { name: "Show all", exact: true }).click();
  await page.keyboard.press("Escape");
  expect(await page.getByRole("columnheader").count()).toBeGreaterThan(10);
  await page.getByRole("button", { name: "Wrap", exact: true }).click();
  await expect(page.locator(".sheet-scroll")).toHaveAttribute("data-wrap", "true");
  await page.getByLabel("Group this page").selectOption("kind");
  await expect(page.locator(".sheet-group-row").first()).toContainText("rows on this page");
  await page.getByLabel("Search all records").fill("no-such-record-391023");
  await expect(dataRows(page)).toHaveCount(0);
  await page.getByLabel("Search all records").fill(first!);
  await expect(dataRows(page).first()).toHaveAttribute("data-record-id", first!);
});

test("replay uses the real predecessor, then settles on the diff", async ({ page }) => {
  await page.goto("/");
  await stage(page, "Privacy");
  await page.getByRole("button", { name: "Replay change" }).click();
  await expect(page.locator(".transformation-table")).toHaveAttribute("data-phase", "before");
  await expect(page.locator(".data-sheet del")).toHaveCount(0);
  await expect(page.locator(".transformation-table")).toHaveAttribute("data-phase", "settled");
  await expect(page.locator('td[data-cell-change="changed"]').first()).toBeVisible();
});

test("expanded sheet, keyboard cell navigation, and mobile containment", async ({ page }) => {
  await page.goto("/");
  await openTable(page);
  await expect(dataRows(page)).toHaveCount(100);
  const first = page.locator(".sheet-cell").first();
  await first.focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".sheet-cell").nth(1)).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Selected cell details")).toBeVisible();
  await page.getByRole("button", { name: "Close cell details" }).click();
  await page.getByRole("button", { name: "Expand table" }).click();
  await expect(page.getByRole("dialog").getByRole("table")).toBeVisible();
  await page.screenshot({ path: shot("expanded"), animations: "disabled" });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await expect(page.getByRole("button", { name: /Drop data or choose files/i })).toBeVisible();
  await page.screenshot({ path: shot("mobile"), fullPage: true });
});

test("review updates the table and downloaded output without stale branches", async ({ page, request }) => {
  await page.goto("/");
  const review = page.getByRole("button", { name: /Review \d+ items/i });
  const before = Number((await review.innerText()).match(/\d+/)?.[0]);
  await review.click();
  const dialog = page.getByRole("dialog");
  let id = "";
  for (let n = 0; n < 10; n++) {
    id = await dialog.locator(".review-card code").first().innerText();
    const saved = page.waitForResponse(response => response.url().includes("/api/v1/reviews/") && response.request().method() === "PATCH");
    await dialog.getByRole("button", { name: "Accept", exact: true }).first().click();
    expect((await saved).ok()).toBeTruthy();
    await expect(dialog.locator(".review-card code").first()).not.toHaveText(id);
  }
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(review).toHaveText(`Review ${before - 10} items`);
  await stage(page, "Ready");
  await page.getByLabel("Search all records").fill(id);
  await expect(dataRows(page).first()).toHaveAttribute("data-record-id", id);
  await stage(page, "Review");
  await page.getByLabel("Search all records").fill(id);
  await expect(dataRows(page)).toHaveCount(0);
  const ws = await (await request.get("http://127.0.0.1:8100/api/v1/workspace")).json();
  const output = ws.outputs.find((item: { format: string; name: string }) => item.format === "jsonl" && item.name === "Ready records");
  expect(await (await request.get(`http://127.0.0.1:8100/api/v1/outputs/${output.id}`)).text()).toContain(`"event_id":"${id}"`);
});

test("workspace options remain usable", async ({ page }) => {
  await page.goto("/");
  for (const action of ["Run history", "Workflow details", "Automation"]) {
    await page.getByRole("button", { name: "Workspace options" }).click();
    await page.getByRole("menuitem", { name: action }).click();
    await expect(page.getByRole("dialog").getByRole("heading", { name: action, exact: true })).toBeVisible();
    await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();
  }
});

test("dark appearance, reduced motion, and network retry", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.goto("/");
  await openTable(page);
  await expect(dataRows(page)).toHaveCount(100);
  await expect(page.locator(".single-workspace")).toHaveCount(1);
  await expect(page.locator(".radix-themes").first()).toHaveClass(/dark/);
  await stage(page, "Normalize");
  await page.getByRole("button", { name: "Replay change" }).click();
  await expect(page.locator(".transformation-table")).toHaveAttribute("data-phase", "settled");
  await page.screenshot({ path: shot("dark"), fullPage: true, animations: "disabled" });
  await page.route("**/api/v1/stages/quality/table?**", route => route.fulfill({ status: 503, body: "unavailable" }));
  await page.locator(".pipeline-flow").getByRole("button", { name: /Quality/ }).click();
  await expect(page.locator('.sheet-empty[role="alert"]')).toContainText("could not be read");
  await expect(dataRows(page)).toHaveCount(0);
  await page.unroute("**/api/v1/stages/quality/table?**");
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await openTable(page);
  await expect(dataRows(page).first()).toBeVisible();
  expect(errors).toEqual([]);
});
