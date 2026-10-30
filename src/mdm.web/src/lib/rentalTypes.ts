import type { ChecklistAnswers } from "./checklist";

// A borrower's pending request to extend a rental's expected return date
// (續借); it only takes effect once a custodian/admin approves it.
export interface RentalExtension {
  id: string;
  rental_number: number;
  requested_by_name: string;
  previous_expected_return: string;
  requested_expected_return: string;
  reason: string;
  status: string;
  created_at: string;
}

// Shared between the desktop admin view (Rentals.tsx) and the mobile
// self-service pages — both read the same GET /api/rentals response and
// group it into per-rental_number batches (every asset in a batch shares
// one rental_number and moves through the workflow together).
export interface Rental {
  id: string;
  asset_id: string | null;
  device_udid: string | null;
  asset_number: string;
  asset_name: string;
  borrower_id: string;
  borrower_name: string;
  approver_id?: string;
  approver_name: string;
  custodian_id?: string;
  custodian_name: string;
  status: string;
  purpose: string;
  borrow_date: string;
  expected_return?: string;
  actual_return?: string;
  notes: string;
  device_name: string;
  device_serial: string;
  rental_number: number;
  is_archived: boolean;
  category_id?: string | null;
  return_checklist?: ChecklistAnswers;
  return_notes?: string;
  return_checklist_reported?: ChecklistAnswers;
  cross_day_reason?: string;
  multi_day_reason?: string;
  period?: "full" | "am" | "pm";
  daily_tracking_required?: boolean;
  pending_extension?: RentalExtension | null;
  // 派發系統（整合 API）建立的預約才有確切時段、單號、乘客與目的地
  created_via?: "web" | "api";
  start_at?: string | null;
  end_at?: string | null;
  external_ref?: string | null;
  passengers?: string[];
  destination?: RentalDestination | null;
  cancel_reason?: string;
  borrower_email?: string;
}

export interface RentalDestination {
  address?: string;
  city?: string;
  district?: string;
}

export interface RentalGroup {
  rental_number: number;
  rentals: Rental[];
  borrower_id: string;
  borrower_name: string;
  purpose: string;
  status: string;
  borrow_date: string;
  expected_return?: string;
  actual_return?: string;
  approver_name: string;
  is_archived: boolean;
  custodian_name: string;
  custodian_id?: string;
  return_checklist?: ChecklistAnswers;
  return_notes?: string;
  return_checklist_reported?: ChecklistAnswers;
  cross_day_reason?: string;
  multi_day_reason?: string;
  period?: "full" | "am" | "pm";
  daily_tracking_required?: boolean;
  pending_extension?: RentalExtension | null;
  created_via?: "web" | "api";
  start_at?: string | null;
  end_at?: string | null;
  external_ref?: string | null;
  passengers?: string[];
  destination?: RentalDestination | null;
  cancel_reason?: string;
  borrower_email?: string;
  notes?: string;
}

export interface DailyReport {
  id: string;
  report_date: string;
  checklist: Record<string, unknown>;
  backfill_reason: string;
  reported_at: string;
}

function pickBookingDetails(items: Rental[]) {
  const r = items.find((it) => it.created_via === "api") ?? items[0];
  return {
    created_via: r.created_via,
    start_at: r.start_at,
    end_at: r.end_at,
    external_ref: r.external_ref,
    passengers: r.passengers,
    destination: r.destination,
    cancel_reason: r.cancel_reason,
    borrower_email: r.borrower_email,
    notes: r.notes,
  };
}

export function groupByRentalNumber(rentals: Rental[]): RentalGroup[] {
  const map = new Map<number, Rental[]>();
  for (const r of rentals) {
    const list = map.get(r.rental_number) || [];
    list.push(r);
    map.set(r.rental_number, list);
  }
  const groups: RentalGroup[] = [];
  for (const [num, items] of map) {
    const first = items[0];
    groups.push({
      rental_number: num,
      rentals: items,
      borrower_id: first.borrower_id,
      borrower_name: first.borrower_name,
      purpose: first.purpose,
      status: first.status,
      borrow_date: first.borrow_date,
      expected_return: first.expected_return,
      actual_return: first.actual_return,
      approver_name: first.approver_name,
      is_archived: first.is_archived,
      custodian_name: first.custodian_name,
      custodian_id: first.custodian_id,
      return_checklist: first.return_checklist,
      return_notes: first.return_notes,
      return_checklist_reported: first.return_checklist_reported,
      cross_day_reason: first.cross_day_reason,
      multi_day_reason: first.multi_day_reason,
      period: first.period,
      pending_extension: first.pending_extension,
      // A booking made through the dispatch system carries its own details; in a
      // batch that mixes one with other assets, they belong to that rental.
      ...pickBookingDetails(items),
      // Union across the batch, matching the backend's own union policy for
      // "does this batch touch any daily-tracking category".
      daily_tracking_required: items.some((it) => it.daily_tracking_required),
    });
  }
  groups.sort((a, b) => b.rental_number - a.rental_number);
  return groups;
}

export const periodLabel: Record<string, string> = { full: "全天", am: "上午", pm: "下午" };
