import { describe, expect, it } from "vitest";
import type { Werte } from "./api";
import {
  bezugsorteOptions,
  bundeslaenderOf,
  kistSatzFor,
  mahlzeitLabelOf,
  mealCodesOf,
  sbwFor,
} from "./werte";

const werte: Werte = {
  sachbezugswerte: [
    {
      jahr: 2026,
      fruehstueck_cent: 237,
      mittag_cent: 457,
      abend_cent: 457,
      status: "amtlich",
    },
    {
      jahr: 2027,
      fruehstueck_cent: 243,
      mittag_cent: 470,
      abend_cent: 470,
      status: "entwurf",
    },
  ],
  kirchensteuer: [
    { land: "BW", satz_bp: 450 },
    { land: "BE", satz_bp: 500 },
  ],
  bundeslaender: ["BW", "BE"],
  mahlzeiten: [
    { code: "fruehstueck", label: "Frühstück" },
    { code: "mittag", label: "Mittag" },
  ],
  bezugsorte: [
    { code: "supermarkt", label: "Supermarkt" },
    { code: "baeckerei", label: "Bäckerei" },
  ],
};

describe("werte helpers", () => {
  it("resolves meal labels and purchase-place options", () => {
    expect(mahlzeitLabelOf(werte, "mittag")).toBe("Mittag");
    expect(bezugsorteOptions(werte)).toEqual([
      ["supermarkt", "Supermarkt"],
      ["baeckerei", "Bäckerei"],
    ]);
    expect(bundeslaenderOf(werte)).toEqual(["BW", "BE"]);
    expect(mealCodesOf(werte)).toEqual(["fruehstueck", "mittag"]);
  });

  it("never returns a raw code while the reference table is loading", () => {
    expect(mahlzeitLabelOf(undefined, "mittag")).toBeUndefined();
    expect(bezugsorteOptions(undefined)).toEqual([]);
    expect(bundeslaenderOf(undefined)).toEqual([]);
    expect(mealCodesOf(undefined)).toEqual([]);
    expect(kistSatzFor(undefined, "BE", 700)).toBe(700);
    expect(sbwFor(undefined, 2027)).toBeUndefined();
  });

  it("falls back to the code for a meal the server does not label", () => {
    expect(mahlzeitLabelOf(werte, "abend")).toBe("abend");
  });

  it("keeps the caller's rate for a Bundesland without a suggestion", () => {
    expect(kistSatzFor(werte, "BE", 700)).toBe(500);
    expect(kistSatzFor(werte, "XX", 123)).toBe(123);
  });

  it("looks the Sachbezugswerte up by year", () => {
    expect(sbwFor(werte, 2027)?.mittag_cent).toBe(470);
    expect(sbwFor(werte, 2025)).toBeUndefined();
  });

  // A nil array from the server must not throw; the helpers degrade instead.
  it("survives a payload whose lists are null", () => {
    const kaputt = {
      sachbezugswerte: null,
      kirchensteuer: null,
      bundeslaender: null,
      mahlzeiten: null,
      bezugsorte: null,
    } as unknown as Werte;

    expect(kistSatzFor(kaputt, "BE", 700)).toBe(700);
    expect(sbwFor(kaputt, 2027)).toBeUndefined();
    expect(bezugsorteOptions(kaputt)).toEqual([]);
    expect(bundeslaenderOf(kaputt)).toEqual([]);
    expect(mealCodesOf(kaputt)).toEqual([]);
    expect(mahlzeitLabelOf(kaputt, "mittag")).toBe("mittag");
  });
});
