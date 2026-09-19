import { useEffect, useState } from "react";
import { Link } from "react-router";
import { Clock } from "lucide-react";
import apiClient from "../../lib/apiClient";
import { useAuthStore } from "../../stores/authStore";
import { MobileShell } from "../../components/mobile/MobileShell";
import { usePendingActions, useOnlineStatus, flush, discardAction } from "../../lib/offlineQueue";
import { type Rental, groupByRentalNumber } from "../../lib/rentalTypes";

const statusBadge: Record<string, string> = {
  active: "badge-warning",
  pending_return: "badge-info",
};
const statusLabel: Record<string, string> = {
  active: "借出中",
  pending_return: "待核對",
};

export function MobileHome() {
  const { user } = useAuthStore();
  const online = useOnlineStatus();
  const pending = usePendingActions();
  const [rentals, setRentals] = useState<Rental[]>([]);
  const [loading, setLoading] = useState(true);

  const load = async () => {
    try {
      const { data } = await apiClient.get("/api/rentals");
      setRentals(data.rentals || []);
    } catch { /* offline or a transient error — the pending-sync card above still shows queued work */ }
    finally { setLoading(false); }
  };

  useEffect(() => { load(); }, []);
  // Refresh the list once queued items finish syncing so a card's status
  // (e.g. active -> pending_return after submit-return goes through) updates.
  useEffect(() => { if (online && pending.length === 0) load(); }, [online, pending.length]);

  const myGroups = groupByRentalNumber(rentals)
    .filter((g) => g.borrower_id === user?.id && (g.status === "active" || g.status === "pending_return"));

  const pendingByRental = new Map<string, number>();
  for (const p of pending) pendingByRental.set(p.rentalId, (pendingByRental.get(p.rentalId) || 0) + 1);

  return (
    <MobileShell title="我的租借">
      {pending.length > 0 && (
        <div className="rounded-2xl border border-warning/40 bg-warning/10 p-4 flex flex-col gap-2">
          <div className="flex items-center gap-2 text-warning-content">
            <Clock size={16} />
            <span className="text-sm font-bold">{pending.length} 筆資料待同步</span>
          </div>
          <div className="text-xs opacity-80 leading-relaxed">
            已存在裝置上，恢復連線（含公司 VPN）後會自動送出，也可以手動重試。
          </div>
          {pending.some((p) => p.status === "failed") && (
            <ul className="text-xs text-error flex flex-col gap-1">
              {pending.filter((p) => p.status === "failed").map((p) => (
                <li key={p.id} className="flex items-center justify-between gap-2">
                  <span>單號 {p.rentalNumber}：{p.error}</span>
                  <button className="btn btn-ghost btn-xs" onClick={() => discardAction(p.id)}>捨棄</button>
                </li>
              ))}
            </ul>
          )}
          <button type="button" className="btn btn-outline btn-warning btn-sm" disabled={!online} onClick={() => flush()}>
            立即重試同步
          </button>
        </div>
      )}

      <div className="text-xs font-bold text-base-content/50 uppercase tracking-wide">進行中的租借</div>

      {loading ? (
        <div className="flex justify-center py-12"><span className="loading loading-spinner"></span></div>
      ) : myGroups.length === 0 ? (
        <div className="text-center py-12 text-sm text-base-content/50">目前沒有進行中的租借</div>
      ) : (
        myGroups.map((g) => {
          const rentalId = g.rentals[0].id;
          const queuedCount = pendingByRental.get(rentalId) || 0;
          return (
            <div key={g.rental_number} className="card bg-base-100 border border-base-300 shadow-sm">
              <div className="card-body p-4 gap-3">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="font-bold text-sm">
                      {g.rentals.map((r) => r.asset_name).join("、")}
                      <span className="font-normal text-base-content/50 text-xs">　{g.rentals.map((r) => r.asset_number).join(", ")}</span>
                    </div>
                    <div className="text-xs text-base-content/40 mt-0.5">單號 {g.rental_number}</div>
                  </div>
                  <span className={`badge ${statusBadge[g.status]} badge-sm whitespace-nowrap`}>{statusLabel[g.status]}</span>
                </div>

                {g.status === "active" ? (
                  <>
                    <div className="text-xs text-base-content/60">
                      借出 {g.borrow_date?.slice(0, 10)}　→　預計 {g.expected_return?.slice(0, 10) || "—"}
                    </div>
                    <div className="flex gap-2">
                      {g.daily_tracking_required && (
                        <Link to={`/m/rentals/${rentalId}/daily-report`} className="btn btn-primary btn-sm flex-1 relative">
                          今日回報
                          {queuedCount > 0 && (
                            <span className="absolute -top-1.5 -right-1.5 badge badge-error badge-xs">{queuedCount}</span>
                          )}
                        </Link>
                      )}
                      <Link to={`/m/rentals/${rentalId}/submit-return`} className="btn btn-outline btn-sm flex-1">我要歸還</Link>
                    </div>
                  </>
                ) : (
                  <div className="text-xs text-base-content/60">已送出歸還回報，等待保管人核對</div>
                )}
              </div>
            </div>
          );
        })
      )}
    </MobileShell>
  );
}
