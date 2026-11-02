// Rentals made through the dispatch system have an exact time span (start_at /
// end_at, ISO 8601 with the Taipei +08:00 offset, so the clock can be read
// straight off the string); every other rental only has dates.

interface Span {
  start_at?: string | null;
  end_at?: string | null;
  borrow_date: string;
  expected_return?: string;
  period?: "full" | "am" | "pm";
}

const md = (iso: string) => `${iso.slice(5, 7)}/${iso.slice(8, 10)}`;
const hm = (iso: string) => iso.slice(11, 16);
const periodText = { full: "全天", am: "上午", pm: "下午" } as const;

/** Today's date in Taipei, YYYY-MM-DD. */
export const taipeiToday = () => new Date().toLocaleDateString("sv-SE", { timeZone: "Asia/Taipei" });

/** The calendar day (YYYY-MM-DD) the rental starts on. */
export function rentalStartDate(r: Span): string {
  return r.start_at ? r.start_at.slice(0, 10) : r.borrow_date.slice(0, 10);
}

/** "11/26 09:16" for a timed rental's start or end, otherwise the date ("11/26"). */
export function rentalPoint(r: Span, which: "start" | "end"): string {
  if (r.start_at && r.end_at) {
    const iso = which === "start" ? r.start_at : r.end_at;
    return `${md(iso)} ${hm(iso)}`;
  }
  const date = which === "start" ? r.borrow_date : r.expected_return;
  return date ? md(date) : "-";
}

/** One-line description of when: "11/26 09:16–17:16", "11/26 全天", "11/26 – 11/28". */
export function rentalSpanText(r: Span): string {
  if (r.start_at && r.end_at) {
    return r.start_at.slice(0, 10) === r.end_at.slice(0, 10)
      ? `${md(r.start_at)} ${hm(r.start_at)}–${hm(r.end_at)}`
      : `${md(r.start_at)} ${hm(r.start_at)} – ${md(r.end_at)} ${hm(r.end_at)}`;
  }
  const start = md(r.borrow_date);
  if (!r.expected_return) return `${start} 起`;
  const end = md(r.expected_return);
  if (start === end) return `${start} ${periodText[r.period || "full"]}`;
  return `${start} – ${end}`;
}
