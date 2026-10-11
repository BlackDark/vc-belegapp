import { expect, login, test } from "../fixtures";

test("settings store the month-export CSV and ZIP defaults", async ({ page }) => {
  await login(page);
  await page.goto("/einstellungen");
  const csv = page.getByRole("checkbox", {
    name: "CSV im Monatsexport vorauswählen",
  });
  const zip = page.getByRole("checkbox", {
    name: "ZIP mit Originalbildern im Monatsexport vorauswählen",
  });
  await expect(csv).toBeChecked();
  await expect(zip).not.toBeChecked();
  await csv.uncheck();
  await zip.check();
  await page.getByRole("button", { name: "Profil speichern" }).click();
  await expect(page.getByText("Gespeichert")).toBeVisible();

  await page.reload();
  await expect(csv).not.toBeChecked();
  await expect(zip).toBeChecked();

  await page.goto("/monat");
  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByRole("checkbox", { name: "CSV" })).not.toBeChecked();
  await expect(
    page.getByRole("checkbox", { name: "ZIP mit Originalbildern" }),
  ).toBeChecked();
});
