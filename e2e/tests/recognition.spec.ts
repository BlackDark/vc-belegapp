import { expect, login, png, saveYear, test } from "../fixtures";

test("recognition, suggestion, corrected amount, save", async ({ page }) => {
  await login(page);
  await saveYear(page, 2026);

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "beleg.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", { timeout: 20_000 });
  await expect(page.getByLabel("Ort", { exact: true })).toHaveValue("Köln");
  await expect(page.getByLabel("Belegbetrag")).toHaveValue("14,90");
  await expect(page.getByTestId("korrekturvorschlag")).toBeVisible();
  await page.getByText("Positionen").click();
  await expect(page.getByText("Shampoo")).toBeVisible();

  await page.getByRole("button", { name: "Übernehmen" }).click();
  await expect(page.getByLabel("Anerkannter Betrag")).toHaveValue("12,75");
  await page.getByLabel("Anerkannter Betrag").fill("10,00");
  await page.getByLabel("Grund").fill("Ohne Getränk");

  await page.getByLabel("Datum").fill("2026-10-07");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();
  await page.getByLabel("Monat").fill("2026-10");
  await page.getByRole("link", { name: /07\.10\. Edeka/ }).click();
  await page.getByText("Korrigierter Betrag").click();
  await expect(page.getByLabel("Anerkannter Betrag")).toHaveValue("10,00");
  await expect(page.getByLabel("Grund")).toHaveValue("Ohne Getränk");
});
