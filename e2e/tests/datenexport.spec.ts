import { expect, type Page, test } from "@playwright/test";

const password = process.env.E2E_PASSWORD ?? "belegapp-e2e";
const importOrigin = `http://127.0.0.1:${process.env.E2E_PORT_IMPORT ?? "8081"}`;

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

// 2024 stays clear of the 2025 export months and the 2026 capture months.
// Each project uses its own month so a shared database does not change the
// other project's totals. A retry moves one day forward inside that month.
function datum(project: string, retry: number) {
  if (project === "pixel-8") {
    return `2024-04-${String(8 + retry).padStart(2, "0")}`;
  }
  return `2024-03-${String(4 + retry).padStart(2, "0")}`;
}

async function shot(page: Page, name: string) {
  const dir = process.env.E2E_SHOT_DIR;
  if (!dir) {
    return;
  }
  await page.screenshot({ path: `${dir}/${name}.png` });
}

test("export, restore on a fresh instance, same month totals", async ({
  page,
}, info) => {
  test.setTimeout(180_000);
  const day = datum(info.project.name, info.retry);
  const month = day.slice(0, 7);
  const shots = info.project.name === "iphone-15" && info.retry === 0;

  await page.goto("/login");
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();

  await page.goto("/einstellungen/jahre/2024");
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
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", {
    timeout: 20_000,
  });
  await page.getByLabel("Datum").fill(day);
  await page.getByLabel("Händler").fill("REWE");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();

  await page.getByLabel("Monat").fill(month);
  const erstattung = (
    await page.getByText(/^Erstattung /).first().innerText()
  ).trim();
  const eigenanteil = (
    await page.getByText(/^Eigenanteil /).first().innerText()
  ).trim();
  const ag = (await page.getByText(/^AG-Kosten /).first().innerText()).trim();
  expect(erstattung).toMatch(/€/);

  await page.goto("/einstellungen");
  await page.getByRole("button", { name: "Datenexport erstellen" }).click();
  await expect(page.getByText("Datenexport ist bereit.")).toBeVisible({
    timeout: 60_000,
  });
  if (shots) {
    await page.getByRole("heading", { name: "Datenexport" }).scrollIntoViewIfNeeded();
    await shot(page, "datenexport");
  }
  const href = await page
    .getByRole("link", { name: "Datenexport herunterladen" })
    .getAttribute("href");
  expect(href).toBeTruthy();
  const downloaded = await page.request.get(href ?? "");
  expect(downloaded.status()).toBe(200);
  const zip = await downloaded.body();
  expect(zip.byteLength).toBeGreaterThan(100);

  await page.goto(`${importOrigin}/login`);
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
  await page.goto(`${importOrigin}/einstellungen`);
  await page.getByLabel("Datenimport").setInputFiles({
    name: "datenexport.zip",
    mimeType: "application/zip",
    buffer: zip,
  });
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText(/Anzahl Belege/)).toBeVisible({
    timeout: 30_000,
  });
  if (shots) {
    await shot(page, "datenimport-bestaetigung");
  }
  await page.getByLabel("Bestätigung").fill("ERSETZEN");
  await page.getByRole("button", { name: "Wiederherstellen" }).click();
  const done = page.getByText(
    "Datenimport abgeschlossen. Alle Sitzungen wurden beendet.",
  );
  await expect(done).toBeVisible({ timeout: 60_000 });
  if (shots) {
    await done.scrollIntoViewIfNeeded();
    await shot(page, "datenimport-ergebnis");
  }

  await page.getByRole("button", { name: "Zur Anmeldung" }).click();
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
  await page.goto(`${importOrigin}/monat`);
  await page.getByLabel("Monat").fill(month);
  await expect(page.getByText(erstattung).first()).toBeVisible();
  await expect(page.getByText(eigenanteil).first()).toBeVisible();
  await expect(page.getByText(ag).first()).toBeVisible();
});
