import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { type Page, type TestInfo } from "@playwright/test";
import { expect, login, saveYear, test } from "../fixtures";

// October 2026 fixture from docs/SPEC.md §6.5 (vector M1). Names are fictional.
// Receipt pixels are drawn in the browser; nothing here is a real Kassenzettel.
const demo = [
  {
    datum: "2026-10-05",
    haendler: "REWE",
    ort: "Köln",
    betrag: 840,
    bezugsort: "supermarkt",
    lines: ["REWE", "Köln", "05.10.2026  12:14", "Mittagessen", "8,40 EUR", "SUMME", "8,40 EUR"],
  },
  {
    datum: "2026-10-06",
    haendler: "Bäckerei Kruse",
    ort: "Köln",
    betrag: 620,
    bezugsort: "baeckerei",
    lines: ["Bäckerei Kruse", "Köln", "06.10.2026  12:02", "Belegtes Brötchen", "6,20 EUR", "SUMME", "6,20 EUR"],
  },
  {
    datum: "2026-10-07",
    haendler: "Edeka",
    ort: "Köln",
    betrag: 1490,
    bezugsort: "supermarkt",
    korrigiert: 1250,
    grund: "Pfand und Drogerie",
    lines: ["Edeka", "Köln", "07.10.2026  12:31", "Mittagstisch     12,50", "Pfand            0,25", "Drogerie         2,15", "SUMME EUR       14,90"],
  },
  {
    datum: "2026-10-08",
    haendler: "Lidl",
    ort: "Köln",
    betrag: 767,
    bezugsort: "supermarkt",
    lines: ["Lidl", "Köln", "08.10.2026  12:08", "Mittagessen", "7,67 EUR", "SUMME", "7,67 EUR"],
  },
  {
    datum: "2026-10-09",
    haendler: "Kiosk",
    ort: "Köln",
    betrag: 380,
    bezugsort: "sonstiges",
    lines: ["Kiosk", "Köln", "09.10.2026  12:20", "Mittagessen", "3,80 EUR", "SUMME", "3,80 EUR"],
  },
];

async function shot(page: Page, info: TestInfo, name: string, fullPage = true) {
  const dir = path.join("screenshots", info.project.name);
  await mkdir(dir, { recursive: true });
  await page.screenshot({ path: path.join(dir, `${name}.png`), fullPage });
}

async function seedDemo(page: Page) {
  const sum = await page.evaluate(async (items) => {
    const fail = async (res: Response, what: string) => {
      const text = await res.text();
      throw new Error(`${what} ${res.status} ${text}`);
    };
    const current = await fetch("/api/v1/einstellungen", { credentials: "include" });
    if (!current.ok) await fail(current, "einstellungen");
    const profile = (await current.json()) as Record<string, unknown>;
    const saved = await fetch("/api/v1/einstellungen", {
      method: "PUT",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        ...profile,
        arbeitnehmer_name: "Alex Beispiel",
        personalnummer: "E-1042",
        arbeitgeber_name: "Beispiel GmbH",
      }),
    });
    if (!saved.ok) await fail(saved, "profil");

    for (const item of items) {
      const canvas = document.createElement("canvas");
      canvas.width = 440;
      canvas.height = 640;
      const ctx = canvas.getContext("2d");
      if (!ctx) throw new Error("canvas");
      ctx.fillStyle = "#fffdf6";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.strokeStyle = "#222";
      ctx.lineWidth = 2;
      ctx.strokeRect(18, 18, 404, 604);
      ctx.fillStyle = "#1a1a1a";
      ctx.font = "22px DejaVu Sans, sans-serif";
      ctx.textAlign = "center";
      for (let i = 0; i < item.lines.length; i++) {
        ctx.fillText(item.lines[i], 220, 72 + i * 36);
      }
      const blob = await new Promise<Blob>((resolve, reject) => {
        canvas.toBlob((value) => (value ? resolve(value) : reject(new Error("toBlob"))), "image/png");
      });
      const body = new FormData();
      body.append("datei", blob, "beleg.png");
      body.append("erkennung", "false");
      const uploaded = await fetch("/api/v1/belegbilder", { method: "POST", body, credentials: "include" });
      if (!uploaded.ok) await fail(uploaded, "bild");
      const bild = (await uploaded.json()) as { id: string };
      const payload: Record<string, unknown> = {
        datum: item.datum,
        mahlzeit: "mittag",
        bezugsort: item.bezugsort,
        arbeitsort: "betrieb",
        haendler_name: item.haendler,
        haendler_ort: item.ort,
        belegbetrag_cent: item.betrag,
        notiz: "",
        bild_ids: [bild.id],
      };
      if (item.korrigiert != null) {
        payload.korrigierter_betrag_cent = item.korrigiert;
        payload.korrektur_grund = item.grund;
      }
      const created = await fetch("/api/v1/belege", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!created.ok) await fail(created, `beleg ${item.datum}`);
    }

    const monat = await fetch("/api/v1/monate/2026-10", { credentials: "include" });
    if (!monat.ok) await fail(monat, "monat");
    const view = (await monat.json()) as { summen: { erstattung_cent: number } };
    return view.summen.erstattung_cent;
  }, demo);
  expect(sum).toBe(3301);
}

test("all pages", async ({ page }, info) => {
  test.setTimeout(240_000);
  await page.addInitScript(() => {
    localStorage.setItem("belegapp-theme", "hell");
  });

  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Belegapp" })).toBeVisible();
  await shot(page, info, "login");

  await login(page);
  await saveYear(page, 2026);
  await seedDemo(page);

  const heute = page.waitForResponse((res) => res.url().includes("/api/v1/monate/") && res.ok());
  await page.goto("/");
  await heute;
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
  const todayShot = page.locator("img[alt='Beleg']");
  if ((await todayShot.count()) > 0) {
    await expect(todayShot).toBeVisible();
    await expect(todayShot).toHaveCSS("object-fit", "contain");
    const box = await todayShot.boundingBox();
    expect(box && box.height > box.width).toBe(true);
  }
  await shot(page, info, "heute");

  await page.goto("/erfassen");
  await expect(page.getByLabel("Galerie")).toBeVisible();
  await shot(page, info, "erfassen");

  await page.goto("/monat");
  await page.getByLabel("Monat").fill("2026-10");
  await expect(page.getByRole("link", { name: /05\.10\. REWE/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /07\.10\. Edeka/ })).toBeVisible();
  await shot(page, info, "monat");

  await page.getByRole("link", { name: /07\.10\. Edeka/ }).click();
  await expect(page.getByRole("heading", { name: "Beleg prüfen" })).toBeVisible();
  await page.getByText("Korrigierter Betrag").click();
  await expect(page.getByLabel("Händler")).toHaveValue("Edeka");
  await expect(page.getByText(/Erstattung/)).toBeVisible();
  await shot(page, info, "pruefen");

  await page.goto("/einstellungen/jahre/2026");
  await expect(page.getByLabel("Zuschuss (Cent)")).toHaveValue("767");
  await shot(page, info, "jahresregel", false);

  await page.goto("/einstellungen");
  await expect(page.getByRole("textbox", { name: "Arbeitnehmer" })).toHaveValue("Alex Beispiel");
  await shot(page, info, "einstellungen", false);

  await page.goto("/monat");
  await page.getByLabel("Monat").fill("2026-10");
  await page.getByRole("button", { name: "Exportieren" }).click();
  await expect(page.getByText("Höchstens ein Beleg je Tag")).toBeVisible();
  await shot(page, info, "monatsexport", false);

  if (info.project.name !== "desktop") return;

  await page.getByRole("checkbox", { name: /Ich versichere/ }).check();
  const finalBtn = page.getByRole("button", { name: "Final exportieren" });
  if (await finalBtn.isDisabled()) {
    await page.getByRole("checkbox", { name: "Warnungen geprüft" }).check();
  }
  await expect(finalBtn).toBeEnabled();
  const done = page.waitForResponse(
    (res) => res.url().includes("/exporte") && res.request().method() === "POST",
  );
  await finalBtn.click();
  expect((await done).status()).toBe(201);
  await expect(page.getByText("Status Gesperrt")).toBeVisible();
  const href = await page.getByRole("link", { name: "PDF" }).getAttribute("href");
  const pdf = await page.request.get(href ?? "");
  expect(pdf.status()).toBe(200);
  const body = await pdf.body();
  expect(body.subarray(0, 5).toString()).toBe("%PDF-");
  await writeFile(path.join("screenshots", "monatsexport.pdf"), body);
});
