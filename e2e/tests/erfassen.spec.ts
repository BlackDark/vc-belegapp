import { expect, login, png, saveYear, test } from "../fixtures";

test("login, jahresregel, upload, month, duplicate, dark mode", async ({ page }) => {
  await login(page);
  await saveYear(page, 2026);

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "beleg.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByRole("img", { name: "Belegbild" })).toBeVisible();
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", { timeout: 20_000 });

  await page.getByLabel("Datum").fill("2026-10-03");
  await page.getByLabel("Händler").fill("REWE");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await expect(page.getByText("Belegtag ist ein Samstag.")).toBeVisible();
  await expect(
    page.getByText("Belegtag ist ein Feiertag: Tag der Deutschen Einheit."),
  ).toBeVisible();

  await page.getByLabel("Datum").fill("2026-10-06");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();
  await page.getByLabel("Monat").fill("2026-10");
  await expect(page.getByText("Mo").first()).toBeVisible();
  await expect(page.getByRole("link", { name: /06\.10\. REWE/ })).toBeVisible();
  await expect(page.getByText(/^Erstattung /).first()).toBeVisible();

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "zweiter.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", { timeout: 20_000 });
  await page.getByLabel("Datum").fill("2026-10-06");
  await page.getByLabel("Händler").fill("Lidl");
  await page.getByLabel("Belegbetrag").fill("3,80");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByText("Für dieses Datum existiert bereits ein Beleg.")).toBeVisible();

  await page.goto("/einstellungen");
  await page.getByRole("tab", { name: "Dunkel" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
});
