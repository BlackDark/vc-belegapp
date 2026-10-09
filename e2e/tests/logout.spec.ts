import { expect, login, test } from "../fixtures";

test("logout returns to the login page", async ({ page }) => {
  await login(page);
  await page.goto("/einstellungen");
  await page.getByRole("button", { name: "Abmelden" }).click();
  await expect(page.getByRole("heading", { name: "Belegapp" })).toBeVisible();
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: "Belegapp" })).toBeVisible();
});
