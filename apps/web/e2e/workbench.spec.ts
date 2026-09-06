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

test("a generic noisy file can be staged as an independent batch", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Data", exact: true }).click();
  await page.getByRole("button", { name: "Add data" }).click();
  await page.locator('input[type="file"]').setInputFiles(
    path.resolve(process.cwd(), "../../sample_data/examples/noisy-records.json"),
  );
  await expect(page.getByText("5 records · 7 fields")).toBeVisible();
  await page.getByRole("button", { name: "Stage batch" }).click();
  await expect(page.getByRole("button", { name: "Staged" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByText("3,847")).toBeVisible();
  await expect(page.locator("tbody").getByText("noisy-records.json")).toBeVisible();
});

test("review resolution updates the shared queue state", async ({ page }) => {
  await page.goto("/");
  await page.locator(".left-rail nav button").filter({ hasText: "Review" }).click();
  await expect(page.getByText("42 uncertain decisions need a person.")).toBeVisible();
  await page.getByRole("button", { name: "Accept" }).click();
  await expect(page.getByText("41 uncertain decisions need a person.")).toBeVisible();
});

test("mobile workspace has accessible navigation without page overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Pipeline", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Workspace navigation" }).getByRole("button", { name: "Review", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
});
