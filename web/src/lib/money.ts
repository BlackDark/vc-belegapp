export function parseEuroToCent(raw: string): number | null {
  const text = raw.trim().replaceAll(" ", "").replaceAll("€", "");
  if (!text) {
    return null;
  }
  let normalized = text;
  if (text.includes(",")) {
    normalized = text.replaceAll(".", "").replace(",", ".");
  }
  if (!/^\d+(\.\d{1,2})?$/.test(normalized)) {
    return null;
  }
  const [whole, frac = ""] = normalized.split(".");
  const cents = Number(whole) * 100 + Number(`${frac}00`.slice(0, 2));
  if (!Number.isSafeInteger(cents)) {
    return null;
  }
  return cents;
}

export function formatCent(cents: number): string {
  return new Intl.NumberFormat("de-DE", {
    style: "currency",
    currency: "EUR",
  }).format(cents / 100);
}
