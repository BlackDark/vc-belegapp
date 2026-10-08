import { expect, test } from "@playwright/test";

const password = process.env.E2E_PASSWORD ?? "belegapp-e2e";

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

test("upload, recognition prefill, suggestion, save", async ({ page }, info) => {
  const day = (info.project.name === "pixel-8" ? 2 : 1) + info.retry * 2;
  const datum = `2026-10-${String(day).padStart(2, "0")}`;

  await page.goto("/login");
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();

  await page.goto("/einstellungen/jahre/2026");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByText("Jahresregel gespeichert")).toBeVisible();

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
  await expect(page.getByLabel("Grund")).toHaveValue(
    "Automatisch: ohne Pfand/Alkohol/Tabak/Non-Food",
  );

  await page.getByLabel("Datum").fill(datum);
  await page.getByRole("button", { name: "Speichern" }).click();
  const stamp = `${datum.slice(8, 10)}.${datum.slice(5, 7)}.`;
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(`${stamp} Edeka`) })).toBeVisible();
});
