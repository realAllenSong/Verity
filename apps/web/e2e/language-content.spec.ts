import path from "node:path";
import os from "node:os";
import { expect, test } from "@playwright/test";

test("actual language content survives upload, normalization, privacy, review and export", async ({ page, request }) => {
  test.setTimeout(120_000);
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  await page.goto("/");
  await page.getByRole("button", { name: /Try a conversation sample/ }).click();
  await expect(page.locator(".job-state strong")).toHaveText("Result ready", { timeout: 60_000 });
  await expect(page.getByRole("textbox", { name: "Search all records" })).toHaveValue("language-demo");
  await expect(page.locator(".text-record")).toHaveCount(13);
  const conversation = page.locator(".text-record").filter({ hasText: "Fix retries without changing the client contract" });
  await expect(conversation.getByText("User prompt", { exact: true })).toBeVisible();
  await expect(conversation.getByText("User correction", { exact: true })).toBeVisible();
  await expect(conversation.getByText(/Stop — don't add another argument/)).toBeVisible();
  await expect(conversation.getByText(/I will move the retry decision/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Choose columns" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Wrap", exact: true })).toHaveCount(0);
  await conversation.getByRole("button", { name: /Show 2 more messages/ }).click();
  await expect(conversation.getByText(/I have not opened a PR or deployed/)).toBeVisible();
  await conversation.getByText("Tool call", { exact: true }).click();
  await expect(conversation.getByText(/go test .\/internal\/client/)).toBeVisible();
  await expect(conversation).not.toContainText("DEMONOTAREALCREDENTIAL");
  await expect(conversation).not.toContainText("PRIVATE_DEMO_DO_NOT_DISPLAY");
  await page.screenshot({ path: path.join(os.tmpdir(), "verity-content-raw.png"), fullPage: true, animations: "disabled" });
  await conversation.getByRole("button", { name: "Inspect fields" }).click();
  await expect(page.getByRole("dialog")).toContainText("messages");
  await expect(page.getByRole("dialog")).not.toContainText("DEMONOTAREALCREDENTIAL");
  await expect(page.getByRole("dialog")).not.toContainText("PRIVATE_DEMO_DO_NOT_DISPLAY");
  await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).click();

  for (const name of ["Normalize", "Privacy", "Quality"]) {
    await page.locator(".pipeline-flow").getByRole("button", { name: new RegExp(name) }).click();
    await expect(conversation).toBeVisible();
    await expect(page.locator(".transformation-table")).toHaveAttribute("data-phase", "settled");
    await expect(conversation.getByText(/Stop — don't add another argument/)).toBeVisible();
    await expect(page.getByRole("textbox", { name: "Search all records" })).toHaveValue("language-demo");
  }
  await page.screenshot({ path: path.join(os.tmpdir(), "verity-content-normalized.png"), fullPage: true, animations: "disabled" });
  await page.locator(".pipeline-flow").getByRole("button", { name: /Extract/ }).click();
  await expect(conversation.locator(".reader-signal")).toBeVisible();
  await conversation.locator(".reader-signal summary").click();
  await expect(conversation.locator("blockquote")).toContainText("Stop — don't add another argument");
  const recordId = await conversation.getAttribute("data-record-id");
  const reviewed = await request.patch(`http://127.0.0.1:8100/api/v1/reviews/${encodeURIComponent(recordId!)}`, { data: { decision: "accepted", note: "Synthetic feedback reviewed for preservation test." } });
  expect(reviewed.ok()).toBeTruthy();
  const workspace = await reviewed.json();
  for (const format of ["csv", "jsonl"]) {
    const output = workspace.outputs.find((item: { format: string; id: string }) => item.format === format && item.id.startsWith("out_ready"));
    const file = await request.get(`http://127.0.0.1:8100/api/v1/outputs/${output.id}`);
    expect(file.ok()).toBeTruthy();
    const text = await file.text();
    expect(text).toContain("Stop — don't add another argument");
    expect(text).toContain(format === "csv" ? "content_blocks_json" : "content_blocks");
    expect(text).not.toContain("DEMONOTAREALCREDENTIAL");
    expect(text).not.toContain("PRIVATE_DEMO_DO_NOT_DISPLAY");
  }

  await page.locator(".pipeline-flow").getByRole("button", { name: /Normalize/ }).click();
  await page.getByRole("textbox", { name: "Search all records" }).fill("language-demo-email");
  await expect(page.locator(".text-record")).toHaveCount(1);
  await expect(page.locator(".message-copy")).toContainText("Please keep send(request) stable.");
  await expect(page.locator(".message-copy")).not.toContainText("<p>");
  await page.screenshot({ path: path.join(os.tmpdir(), "verity-content-email.png"), fullPage: true, animations: "disabled" });
  await page.getByRole("textbox", { name: "Search all records" }).fill("language-demo-slides");
  await expect(page.locator(".text-record")).toHaveCount(1);
  await expect(page.getByText("Before edit", { exact: true })).toBeVisible();
  await expect(page.getByText("After edit", { exact: true })).toBeVisible();
  await page.screenshot({ path: path.join(os.tmpdir(), "verity-content-edit.png"), fullPage: true, animations: "disabled" });
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await expect(page.getByRole("button", { name: "Choose columns" })).toBeVisible();
  await page.getByRole("button", { name: "Reading", exact: true }).click();
  await page.getByRole("textbox", { name: "Search all records" }).fill("DEMONOTAREALCREDENTIAL");
  await expect(page.locator(".text-record")).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("language reader works at mobile size, with reduced motion and dark appearance", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "dark" });
  await page.goto("/");
  await page.getByRole("textbox", { name: "Search all records" }).fill("language-demo-codex");
  await expect(page.locator(".text-record")).toHaveCount(1);
  await expect(page.getByText("User correction", { exact: true })).toBeVisible();
  const widths = await page.evaluate(() => ({ body: document.documentElement.scrollWidth, viewport: window.innerWidth }));
  expect(widths.body).toBeLessThanOrEqual(widths.viewport + 1);
  await page.screenshot({ path: path.join(os.tmpdir(), "verity-content-mobile-dark.png"), fullPage: true, animations: "disabled" });
});
