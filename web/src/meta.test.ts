import { describe, expect, it } from "vitest";
import { appName, appShortName } from "./meta";

describe("meta", () => {
  it("uses the product names", () => {
    expect(appName).toBe("Belegapp");
    expect(appShortName).toBe("Belege");
  });
});
