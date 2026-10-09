import { readFile, readdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, login, png, saveYear, test } from "../fixtures";

test("export failure replaces cheap prüfpunkte with the export-time result", async ({
  page,
  app,
}) => {
  test.setTimeout(180_000);
  const day = "2025-09-16";
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
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka", {
    timeout: 20_000,
  });
  await page.getByLabel("Datum").fill(day);
  await page.getByLabel("Händler").fill("REWE");
  await page.getByLabel("Belegbetrag").fill("8,40");
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByRole("heading", { name: "Monat" })).toBeVisible();

  const image = await findStoredImage(app.dataDir);
  const bytes = await readFile(image);
  bytes[Math.floor(bytes.length / 2)] ^= 0xff;
  await writeFile(image, bytes);

  await page.getByLabel("Monat").fill(month);
  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByText("✓ Jeder Beleg hat vollständige Bilder")).toBeVisible();

  await page.getByRole("checkbox", { name: /Ich versichere/ }).check();
  await page.getByRole("checkbox", { name: "Warnungen geprüft" }).check();
  const failed = page.waitForResponse(
    (res) => res.url().endsWith("/exporte") && res.request().method() === "POST",
  );
  const refreshed = page.waitForResponse(
    (res) => res.url().endsWith("/pruefpunkte") && res.request().method() === "GET",
  );
  await page.getByRole("button", { name: "Final exportieren" }).click();
  expect((await failed).status()).toBe(422);
  expect((await refreshed).ok()).toBeTruthy();
  await expect(page.getByText("✗ Jeder Beleg hat vollständige Bilder")).toBeVisible();
  await expect(page.getByText("✓ Jeder Beleg hat vollständige Bilder")).toHaveCount(0);
});

async function findStoredImage(dataDir: string): Promise<string> {
  const root = path.join(dataDir, "blobs", "bilder");
  const found = await walkJpeg(root);
  if (!found) {
    throw new Error(`no stored image under ${root}`);
  }
  return found;
}

async function walkJpeg(dir: string): Promise<string | null> {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      const nested = await walkJpeg(full);
      if (nested) return nested;
      continue;
    }
    if (entry.name.endsWith(".jpg") && !entry.name.endsWith(".ctype")) {
      return full;
    }
  }
  return null;
}
