import type { ReactNode } from "react";
import type { ICellRendererParams } from "ag-grid-enterprise";
import { Clock, MapPin, Package, Users } from "lucide-react";
import type { RentalGroup } from "../lib/rentalTypes";
import { periodLabel } from "../lib/rentalTypes";
import { rentalSpanText } from "../lib/rentalTime";

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3 py-1 text-sm">
      <dt className="w-16 shrink-0 text-base-content/50">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

const dash = <span className="opacity-40">-</span>;

/** The expanded row of the rentals grid: what the booking is on the left, the assets on the right. */
export function RentalDetailCard({ data: g }: ICellRendererParams<RentalGroup>) {
  if (!g) return null;
  const fromDispatch = g.created_via === "api";
  const dest = g.destination;
  // The address usually already names the city; only add it when it does not.
  const area = dest ? [dest.city, dest.district].filter(Boolean).join("") : "";
  const destText = !dest ? "" : dest.address ? (area && !dest.address.includes(area) ? `${dest.address}（${area}）` : dest.address) : area;
  const people = g.passengers || [];
  const note = g.notes && g.notes !== g.purpose ? g.notes : "";

  return (
    <div className="p-3">
      <div className="rounded-lg border border-base-300 bg-base-100 p-4 grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-4">
        {/* 預約資訊 */}
        <section className="min-w-0">
          <h3 className="flex items-center gap-2 font-semibold mb-1">
            <Clock size={16} className="text-primary" />預約資訊
            {fromDispatch && <span className="badge badge-sm badge-primary badge-outline">派發系統</span>}
          </h3>
          <dl>
            <Field label="時段">
              <span className="font-medium">{rentalSpanText(g)}</span>
              {!g.start_at && g.period && g.period !== "full" && (
                <span className="badge badge-xs badge-outline ml-2">{periodLabel[g.period]}</span>
              )}
            </Field>
            <Field label="借用人">
              <span className="font-medium">{g.borrower_name || dash}</span>
              {g.borrower_email && <span className="opacity-60 ml-2">{g.borrower_email}</span>}
            </Field>
            {(fromDispatch || people.length > 0) && (
              <Field label="乘客">
                {people.length > 0 ? (
                  <span className="inline-flex flex-wrap gap-1">
                    {people.map((p, i) => <span key={`${p}-${i}`} className="badge badge-sm badge-ghost gap-1"><Users size={10} />{p}</span>)}
                  </span>
                ) : dash}
              </Field>
            )}
            {(fromDispatch || destText) && (
              <Field label="目的地">
                {destText ? <span className="inline-flex items-start gap-1"><MapPin size={14} className="mt-0.5 shrink-0 opacity-60" />{destText}</span> : dash}
              </Field>
            )}
            <Field label="用途">{g.purpose || dash}</Field>
            {note && <Field label="備註">{note}</Field>}
            {g.multi_day_reason && <Field label="跨日說明">{g.multi_day_reason}</Field>}
            {fromDispatch && g.external_ref && (
              <Field label="來源單號"><span className="font-mono text-xs">{g.external_ref}</span></Field>
            )}
            <Field label="核准人">{g.approver_name || dash}</Field>
            <Field label="保管人">{g.custodian_name || dash}</Field>
            {g.cancel_reason && <Field label="取消原因">{g.cancel_reason}</Field>}
          </dl>
        </section>

        {/* 資產 */}
        <section className="min-w-0">
          <h3 className="flex items-center gap-2 font-semibold mb-1">
            <Package size={16} className="text-primary" />資產
            <span className="text-sm font-normal text-base-content/50">共 {g.rentals.length} 件</span>
          </h3>
          <ul className="divide-y divide-base-200">
            {g.rentals.map((r) => (
              <li key={r.id} className="py-2 flex items-center gap-2 text-sm">
                <span className="font-medium">{r.device_name || r.asset_name || r.device_serial || r.asset_number || "-"}</span>
                {!r.device_udid && <span className="badge badge-xs badge-outline">獨立</span>}
                <span className="ml-auto font-mono text-xs opacity-60">{r.device_serial || r.asset_number || "-"}</span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  );
}
