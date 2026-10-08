import { expect, test } from "@playwright/test";

const password = process.env.E2E_PASSWORD ?? "belegapp-e2e";

// 1×1 PNG. The server normalises it to JPEG and strips metadata.
const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

test("login, upload, save, month, duplicate, dark mode", async ({ page }, info) => {
  const day = (info.project.name === "pixel-8" ? 6 : 5) + info.retry * 2;
  const datum = `2026-10-${String(day).padStart(2, "0")}`;

  await page.goto("/login");
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();

  await page.goto("/einstellungen/jahre/2026");
  await expect(page.getByText("Zuschuss (Cent)")).toBeVisible();
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByText("Jahresregel gespeichert")).toBeVisible();

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "beleg.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByRole("img", { name: "Belegbild" })).toBeVisible();
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", {
    timeout: 20_000,
  });

  await page.getByLabel("Datum").fill("2026-10-03");
  await page.getByLabel("Händler").fill("REWE");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await expect(page.getByText("Belegtag ist ein Samstag.")).toBeVisible();
  await expect(
    page.getByText("Belegtag ist ein Feiertag: Tag der Deutschen Einheit."),
  ).toBeVisible();

  await page.getByLabel("Datum").fill(datum);
  await page.getByRole("button", { name: "Speichern" }).click();
  const stamp = `${datum.slice(8, 10)}.${datum.slice(5, 7)}.`;
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();
  await expect(page.getByRole("link", { name: new RegExp(`${stamp} REWE`) })).toBeVisible();
  await expect(page.getByText("Erstattung").first()).toBeVisible();

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "zweiter.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", {
    timeout: 20_000,
  });
  await page.getByLabel("Datum").fill(datum);
  await page.getByLabel("Händler").fill("Lidl");
  await page.getByLabel("Belegbetrag").fill("3,80");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(
    page.getByText("Für dieses Datum existiert bereits ein Beleg."),
  ).toBeVisible();

  await page.goto("/einstellungen");
  await page.getByLabel("Darstellung").selectOption("dunkel");
  await expect(page.locator("html")).toHaveClass(/dark/);
});
