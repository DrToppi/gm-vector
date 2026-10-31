export const COMPATIBILITY_ZONE = "Europe/Moscow";
export function supportsPlanningZone(zone: string): boolean {
  if (!zone || zone === "Local") return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: zone }).format(new Date(0));
    return true;
  } catch {
    return false;
  }
}
export function planningToday(zone: string, now = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    calendar: "gregory",
    numberingSystem: "latn",
  }).formatToParts(now);
  const value = (name: string) =>
    parts.find((part) => part.type === name)!.value;
  return value("year") + "-" + value("month") + "-" + value("day");
}
export function planningTimestamp(
  value: string,
  zone: string,
  locale: string,
): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return (
    new Intl.DateTimeFormat(locale, {
      timeZone: zone,
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      timeZoneName: "shortOffset",
    }).format(date) +
    " · " +
    zone
  );
}
