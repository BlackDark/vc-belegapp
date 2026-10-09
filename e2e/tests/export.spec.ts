import { expect, test } from "@playwright/test";

const password = process.env.E2E_PASSWORD ?? "belegapp-e2e";

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

// Capture tests save and use the 2026 year rule. Export locks months of 2025 so
// that lock does not block the other project's 2026 rule save. Each project
// locks its own month. A retry stays inside that month.
function datum(project: string, retry: number) {
  if (project === "pixel-8") {
    const day = 11 + retry * 7;
    return `2025-08-${String(day).padStart(2, "0")}`;
  }
  const day = 15 + retry * 7;
  return `2025-09-${String(day).padStart(2, "0")}`;
}

test("preview, final export, lock, edit, geaendert", async ({ page }, info) => {
  test.setTimeout(180_000);
  const day = datum(info.project.name, info.retry);
  const month = day.slice(0, 7);

  await page.goto("/login");
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();

  await page.goto("/einstellungen/jahre/2025");
  await expect(page.getByText("Zuschuss (Cent)")).toBeVisible();
  await page.getByRole("button", { name: "Speichern" }).click();
  // The other project may already have locked a month of this year. Saving the
  // rule then needs an Änderungsgrund; the stored rule is enough to continue.
  await expect(
    page
      .getByText("Jahresregel gespeichert")
      .or(page.getByText("Für ein gesperrtes Jahr ist ein Änderungsgrund")),
  ).toBeVisible();

  await page.goto("/erfassen");
  await page.getByLabel("Galerie").setInputFiles({
    name: "beleg.png",
    mimeType: "image/png",
    buffer: png,
  });
  await expect(page).toHaveURL(/\/belege\/neu\?bild=/);
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", { timeout: 20_000 });
  await page.getByLabel("Datum").fill(day);
  await page.getByLabel("Händler").fill("REWE");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();

  await page.getByLabel("Monat").fill(month);
  const stamp = `${day.slice(8, 10)}.${day.slice(5, 7)}.`;
  await expect(page.getByRole("link", { name: new RegExp(`${stamp} REWE`) })).toBeVisible();

  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByRole("heading", { name: "Monatsexport" })).toBeVisible();
  await expect(page.getByText("Höchstens ein Beleg je Tag")).toBeVisible();

  const preview = page.waitForResponse(
    (res) => res.url().includes("/vorschau") && res.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Vorschau" }).click();
  const previewRes = await preview;
  expect(previewRes.status()).toBe(200);
  expect(previewRes.headers()["content-type"] ?? "").toContain("application/pdf");
  // Chromium does not expose the body of this PDF response to Playwright.
  // Content-Length is the size the server actually wrote.
  expect(Number(previewRes.headers()["content-length"] ?? 0)).toBeGreaterThan(1000);

  await page.getByRole("checkbox", { name: /Ich versichere/ }).check();
  await page.getByRole("checkbox", { name: "Warnungen geprüft" }).check();
  const done = page.waitForResponse(
    (res) => res.url().endsWith("/exporte") && res.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Final exportieren" }).click();
  expect((await done).status()).toBe(201);
  await expect(page.getByText("Status Gesperrt")).toBeVisible();
  await expect(page.getByRole("link", { name: "PDF" })).toBeVisible();

  await page.getByRole("link", { name: new RegExp(`${stamp} REWE`) }).click();
  await page.getByLabel("Notiz").fill("Nach dem Export");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Änderungsgrund" })).toBeVisible();
  await page.locator("textarea").fill("Korrektur nach Export");
  await page.getByRole("button", { name: "Bestätigen" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();

  await page.getByLabel("Monat").fill(month);
  await expect(page.getByText("Status Geändert")).toBeVisible();
});
