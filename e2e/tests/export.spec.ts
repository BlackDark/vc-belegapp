import { expect, login, png, saveYear, test } from "../fixtures";

test("preview, prüfpunkte, final PDF, lock, change with grund", async ({ page }) => {
  test.setTimeout(180_000);
  const day = "2025-09-15";
  const month = "2025-09";

  await login(page);
  await saveYear(page, 2025);

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
  await expect(page.getByRole("link", { name: /15\.09\. REWE/ })).toBeVisible();

  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByRole("heading", { name: "Monatsexport" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Prüfpunkte" })).toBeVisible();
  await expect(page.getByText("Höchstens ein Beleg je Tag")).toBeVisible();

  const preview = page.waitForResponse(
    (res) => res.url().includes("/vorschau") && res.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Vorschau" }).click();
  const previewRes = await preview;
  expect(previewRes.status()).toBe(200);
  expect(previewRes.headers()["content-type"] ?? "").toContain("application/pdf");
  expect(Number(previewRes.headers()["content-length"] ?? 0)).toBeGreaterThan(1000);

  await page.getByRole("checkbox", { name: /Ich versichere/ }).check();
  await page.getByRole("checkbox", { name: "Warnungen geprüft" }).check();
  const done = page.waitForResponse(
    (res) => res.url().endsWith("/exporte") && res.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Final exportieren" }).click();
  expect((await done).status()).toBe(201);
  await expect(page.getByText("Status Gesperrt")).toBeVisible();
  const pdfLink = page.getByRole("link", { name: "PDF" });
  await expect(pdfLink).toBeVisible();
  const pdf = await page.request.get((await pdfLink.getAttribute("href")) ?? "");
  expect(pdf.status()).toBe(200);
  expect(pdf.headers()["content-type"] ?? "").toContain("pdf");
  expect((await pdf.body()).subarray(0, 5).toString()).toBe("%PDF-");

  await page.getByRole("link", { name: /15\.09\. REWE/ }).click();
  await page.getByLabel("Notiz").fill("Nach dem Export");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Änderungsgrund" })).toBeVisible();
  await page.locator("textarea").fill("Korrektur nach Export");
  await page.getByRole("button", { name: "Bestätigen" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();

  await page.getByLabel("Monat").fill(month);
  await expect(page.getByText("Status Geändert")).toBeVisible();
});
