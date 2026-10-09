import { describe, expect, it } from "vitest";
import { formatCent, formatCentInput, parseEuroToCent } from "./money";

describe("parseEuroToCent", () => {
  it("accepts German and dot decimals", () => {
    expect(parseEuroToCent("7,67")).toBe(767);
    expect(parseEuroToCent("7.67")).toBe(767);
    expect(parseEuroToCent("1.490,50")).toBe(149050);
    expect(parseEuroToCent("12")).toBe(1200);
  });

  it("rejects junk", () => {
    expect(parseEuroToCent("")).toBeNull();
    expect(parseEuroToCent("7,678")).toBeNull();
    expect(parseEuroToCent("abc")).toBeNull();
  });
});

describe("formatCentInput", () => {
  it("uses a German decimal without a currency sign", () => {
    expect(formatCentInput(767)).toBe("7,67");
    expect(formatCentInput(100)).toBe("1,00");
  });
});

describe("formatCent", () => {
  it("formats euros", () => {
    expect(formatCent(767)).toContain("7,67");
  });
});
