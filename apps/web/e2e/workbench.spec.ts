import path from "node:path";
import { expect, test } from "@playwright/test";

test("primary workspace pages are real navigable views", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Workflow signals" })).toBeVisible();

  for (const [navigation, heading] of [
    ["Data", "Data"],
    ["Recipes", "Recipe v12"],
    ["Runs", "Runs"],
    ["Review", "Review"],
    ["Outputs", "Outputs"],
  ] as const) {
    await page.locator(".left-rail nav button").filter({ hasText: navigation }).click();
    await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
  }
});

test("a noisy batch stays readable from raw input through final routing", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") consoleErrors.push(message.text()); });
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  await page.goto("/");
  await page.getByRole("button", { name: "Data", exact: true }).click();
  await page.getByRole("button", { name: "Add data" }).click();
  await page.locator('input[type="file"]').setInputFiles(
    path.resolve(process.cwd(), "../../sample_data/examples/noisy-workflow-events.json"),
  );
  await expect(page.getByText("14 records · 30 fields")).toBeVisible();
  await expect(page.getByText("Developer steered the coding agent to reuse the repository adapter pattern.", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "Stage batch" }).click();
  await page.getByRole("button", { name: "Done" }).click();
  await expect(page.getByText("3,856")).toBeVisible();
  await expect(page.locator("tbody").getByText("noisy-workflow-events.json")).toBeVisible();

  await page.getByRole("button", { name: "Pipeline", exact: true }).click();
  await page.getByRole("button", { name: "Run pipeline" }).click();
  await expect(page.getByText("Pipeline completed")).toBeVisible();
  await expect(page.getByRole("button", { name: "Raw 3,856" })).toBeVisible();

  await page.getByRole("button", { name: "Raw 3,856" }).click();
  await expect(page.getByText("trial_evt_013")).toBeVisible();
  await expect(page.getByText("Captured as received").first()).toBeVisible();

  await page.getByRole("button", { name: /Normalize 3,624/ }).click();
  await expect(page.getByText("Duplicate record ID removed.").first()).toBeVisible();

  await page.getByRole("button", { name: /Privacy 3,288/ }).click();
  await expect(page.getByText("Sensitive fragment had no task context.").first()).toBeVisible();
  await expect(page.getByText("[email redacted]", { exact: false }).first()).toBeVisible();
  await page.locator(".stage-record-table tbody tr").filter({ hasText: "[email redacted]" }).first().click();
  await expect(page.getByRole("dialog").getByText("Before", { exact: true })).toBeVisible();
  await expect(page.getByRole("dialog").getByText("After", { exact: true })).toBeVisible();
  await expect(page.getByRole("dialog").getByText("View source JSON", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Close" }).click();

  await page.getByRole("button", { name: /Quality 2,923/ }).click();
  await expect(page.getByText("is below 0.75", { exact: false }).first()).toBeVisible();

  await page.getByRole("button", { name: /Extract 1,094/ }).click();
  await expect(page.getByText("No supported signal found.").first()).toBeVisible();

  await page.getByRole("button", { name: /Ready 1,051/ }).click();
  await expect(page.getByText("Routed here").first()).toBeVisible();
  expect(consoleErrors).toEqual([]);
});

test("review resolution updates the shared queue state", async ({ page }) => {
  await page.goto("/");
  await page.locator(".left-rail nav button").filter({ hasText: "Review" }).click();
  await expect(page.getByText("43 uncertain decisions need a person.")).toBeVisible();
  await page.getByRole("button", { name: "Accept" }).click();
  await expect(page.getByText("42 uncertain decisions need a person.")).toBeVisible();
});

test("mobile workspace has accessible navigation without page overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Pipeline", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Workspace navigation" }).getByRole("button", { name: "Review", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
});
