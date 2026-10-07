// "YYYY-MM-DD" helpers shared by the desktop and mobile rental pages. Dates
// are plain calendar days (the API's expected_return is a DATE), so these work
// on ISO strings rather than Date objects with a time zone.

export function nextDay(iso: string): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString().slice(0, 10);
}

// The earliest date a renewal may ask for: the day after the current return
// date, but never in the past (an already-overdue rental can't be "renewed"
// to a date that has also passed). Mirrors domain.ValidateExtensionRequest.
export function minExtendDate(expectedReturn: string | undefined, today: string): string {
  if (!expectedReturn) return today;
  const next = nextDay(expectedReturn);
  return next > today ? next : today;
}
