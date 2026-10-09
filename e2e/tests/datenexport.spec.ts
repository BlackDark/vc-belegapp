import { expect, login, password, png, saveYear, startStack, test } from "../fixtures";

test("datenexport and datenimport round-trip", async ({ page }, info) => {
  test.setTimeout(180_000);
  const day = "2024-03-04";
  const month = "2024-03";

  await login(page);
  await saveYear(page, 2024);

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
  const erstattung = (await page.getByText(/^Erstattung /).first().innerText()).trim();
  const eigenanteil = (await page.getByText(/^Eigenanteil /).first().innerText()).trim();
  const ag = (await page.getByText(/^AG-Kosten /).first().innerText()).trim();
  expect(erstattung).toMatch(/€/);

  await page.goto("/einstellungen");
  await page.getByRole("button", { name: "Datenexport erstellen" }).click();
  await expect(page.getByText("Datenexport ist bereit.")).toBeVisible({ timeout: 60_000 });
  const href = await page.getByRole("link", { name: "Datenexport herunterladen" }).getAttribute("href");
  expect(href).toBeTruthy();
  const downloaded = await page.request.get(href ?? "");
  expect(downloaded.status()).toBe(200);
  const zip = await downloaded.body();
  expect(zip.byteLength).toBeGreaterThan(100);

  const target = await startStack({
    name: `import-${info.workerIndex}`,
    workerIndex: info.workerIndex,
    slot: 8,
    llm: false,
  });
  try {
    await page.goto(`${target.baseURL}/login`);
    await page.getByLabel("Passwort").fill(password);
    await page.getByRole("button", { name: "Anmelden" }).click();
    await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
    await page.goto(`${target.baseURL}/einstellungen`);
    await page.getByLabel("Datenimport").setInputFiles({
      name: "datenexport.zip",
      mimeType: "application/zip",
      buffer: zip,
    });
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText(/Anzahl Belege/)).toBeVisible({ timeout: 30_000 });
    await page.getByLabel("Bestätigung").fill("ERSETZEN");
    await page.getByRole("button", { name: "Wiederherstellen" }).click();
    await expect(
      page.getByText("Datenimport abgeschlossen. Alle Sitzungen wurden beendet."),
    ).toBeVisible({ timeout: 60_000 });

    await page.getByRole("button", { name: "Zur Anmeldung" }).click();
    await page.getByLabel("Passwort").fill(password);
    await page.getByRole("button", { name: "Anmelden" }).click();
    await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
    await page.goto(`${target.baseURL}/monat`);
    await page.getByLabel("Monat").fill(month);
    await expect(page.getByText(erstattung).first()).toBeVisible();
    await expect(page.getByText(eigenanteil).first()).toBeVisible();
    await expect(page.getByText(ag).first()).toBeVisible();
  } finally {
    await target.stop();
  }
});
