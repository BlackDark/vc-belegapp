export function todayISO(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Europe/Berlin" }).format(
    new Date(),
  );
}

export function currentMonth(): string {
  return todayISO().slice(0, 7);
}
