import { mkdir } from "node:fs/promises";
import path from "node:path";
import { type Page, type TestInfo } from "@playwright/test";
import { expect, login, png, saveYear, test } from "../fixtures";

async function shot(page: Page, info: TestInfo, name: string, fullPage = true) {
  const dir = path.join("screenshots", info.project.name);
  await mkdir(dir, { recursive: true });
  await page.screenshot({ path: path.join(dir, `${name}.png`), fullPage });
}

test("all pages", async ({ page }, info) => {
  test.setTimeout(180_000);

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Belegapp" })).toBeVisible();
  await shot(page, info, "login");

  await login(page);
  await shot(page, info, "heute");

  await page.goto("/einstellungen/jahre/2026");
  await expect(page.getByText("Zuschuss (Cent)")).toBeVisible();
  await shot(page, info, "jahresregel");
  await saveYear(page, 2026);

  await page.goto("/einstellungen");
  await expect(page.getByRole("heading", { name: "Datenexport" })).toBeVisible();
  await shot(page, info, "einstellungen");

  await page.goto("/erfassen");
  await expect(page.getByLabel("Galerie")).toBeVisible();
  await shot(page, info, "erfassen");

  await page.getByLabel("Galerie").setInputFiles({
    name: "beleg.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", { timeout: 20_000 });
  await page.getByLabel("Datum").fill("2026-10-06");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await shot(page, info, "pruefen", false);

  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();
  await page.getByLabel("Monat").fill("2026-10");
  await expect(page.getByRole("link", { name: /06\.10\./ })).toBeVisible();
  await shot(page, info, "monat");

  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByRole("heading", { name: "Prüfpunkte" })).toBeVisible();
  await shot(page, info, "monatsexport", false);
});
