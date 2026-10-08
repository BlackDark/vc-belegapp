import { describe, expect, it } from "vitest";
import { formatCent, parseEuroToCent } from "./money";

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

describe("formatCent", () => {
  it("formats euros", () => {
    expect(formatCent(767)).toContain("7,67");
  });
});
