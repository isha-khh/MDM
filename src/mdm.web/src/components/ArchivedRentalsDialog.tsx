import { useCallback, useEffect, useMemo, useState } from "react";
import { Archive, ArchiveRestore, Search, X } from "lucide-react";
import apiClient from "../lib/apiClient";
import { groupByRentalNumber, type Rental, type RentalGroup } from "../lib/rentalTypes";
import { rentalSpanText } from "../lib/rentalTime";
import { useDialog } from "./DialogProvider";

const STATUS: Record<string, { label: string; badge: string }> = {
  pending: { label: "待核准", badge: "badge-warning" },
  approved: { label: "已核准", badge: "badge-info" },
  active: { label: "借出中", badge: "badge-success" },
  pending_return: { label: "待核對", badge: "badge-info" },
  returned: { label: "已歸還", badge: "badge-ghost" },
  rejected: { label: "已拒絕", badge: "badge-error" },
  cancelled: { label: "已取消", badge: "badge-ghost" },
};

const assetName = (r: Rental) => r.device_name || r.asset_name || r.device_serial || r.asset_number || "-";

interface Props {
  open: boolean;
  onClose: () => void;
  /** Called after rentals were restored, so the page can reload its own list. */
  onRestored: () => void;
}

/** Lists the archived rentals (存查) and puts the ticked ones back into the normal list. */
export function ArchivedRentalsDialog({ open, onClose, onRestored }: Props) {
  const dialog = useDialog();
  const [groups, setGroups] = useState<RentalGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const { data } = await apiClient.get<{ rentals: Rental[] }>("/api/rentals", { params: { show_archived: "true" } });
      setGroups(groupByRentalNumber((data.rentals || []).filter((r) => r.is_archived)));
      setSelected(new Set());
    } catch (err) {
      await dialog.error("載入已存查的租借失敗: " + (err instanceof Error ? err.message : ""));
    } finally {
      setLoading(false);
    }
  }, [dialog]);

  useEffect(() => {
    if (open) {
      setQuery("");
      load();
    }
  }, [open, load]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return groups;
    return groups.filter((g) =>
      [String(g.rental_number), g.borrower_name, g.purpose, g.external_ref || "", ...g.rentals.map(assetName)]
        .some((s) => s.toLowerCase().includes(q)));
  }, [groups, query]);

  const shownSelected = shown.filter((g) => selected.has(g.rental_number));
  const allShownSelected = shown.length > 0 && shownSelected.length === shown.length;

  const toggle = (n: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(n)) next.delete(n);
      else next.add(n);
      return next;
    });

  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      for (const g of shown) {
        if (allShownSelected) next.delete(g.rental_number);
        else next.add(g.rental_number);
      }
      return next;
    });

  const restore = async () => {
    const picked = groups.filter((g) => selected.has(g.rental_number));
    if (picked.length === 0) return;
    if (!(await dialog.confirm(`把選取的 ${picked.length} 筆租借還原到一般列表？`))) return;
    setRestoring(true);
    try {
      await apiClient.post("/api/rentals-unarchive", { ids: picked.flatMap((g) => g.rentals.map((r) => r.id)) });
      await load();
      onRestored();
    } catch (err) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
      await dialog.error("還原失敗: " + (msg || (err instanceof Error ? err.message : "")));
    } finally {
      setRestoring(false);
    }
  };

  return (
    <dialog className={`modal ${open ? "modal-open" : ""}`}>
      <div className="modal-box max-w-5xl">
        <div className="flex items-center gap-2 mb-3">
          <Archive size={18} className="text-primary" />
          <h3 className="font-bold text-lg">已存查的租借</h3>
          <span className="text-sm text-base-content/50">共 {groups.length} 筆</span>
          <button onClick={onClose} className="btn btn-ghost btn-sm btn-circle ml-auto" aria-label="關閉"><X size={16} /></button>
        </div>

        <label className="input input-bordered input-sm flex items-center gap-2 mb-3">
          <Search size={14} className="opacity-50" />
          <input className="grow" placeholder="搜尋單號、借用人、用途、資產" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>

        <div className="overflow-auto max-h-[55vh] border border-base-300 rounded-lg">
          <table className="table table-sm table-pin-rows">
            <thead>
              <tr>
                <th className="w-8">
                  <input type="checkbox" className="checkbox checkbox-xs" checked={allShownSelected} onChange={toggleAll} disabled={shown.length === 0} aria-label="全選" />
                </th>
                <th>單號</th><th>資產</th><th>借用人</th><th>用途</th><th>狀態</th><th>時段</th><th>最後更新</th>
              </tr>
            </thead>
            <tbody>
              {loading && <tr><td colSpan={8} className="text-center"><span className="loading loading-spinner loading-sm" /></td></tr>}
              {!loading && shown.length === 0 && (
                <tr><td colSpan={8} className="text-center text-base-content/50 py-6">{groups.length === 0 ? "沒有已存查的租借" : "沒有符合的租借"}</td></tr>
              )}
              {!loading && shown.map((g) => {
                const st = STATUS[g.status] || STATUS.pending;
                return (
                  <tr key={g.rental_number} className="hover cursor-pointer" onClick={() => toggle(g.rental_number)}>
                    <td onClick={(e) => e.stopPropagation()}>
                      <input type="checkbox" className="checkbox checkbox-xs" checked={selected.has(g.rental_number)} onChange={() => toggle(g.rental_number)} />
                    </td>
                    <td className="font-mono font-medium">{g.rental_number}</td>
                    <td>
                      {assetName(g.rentals[0])}
                      {g.rentals.length > 1 && <span className="badge badge-sm badge-outline ml-1">共 {g.rentals.length} 件</span>}
                    </td>
                    <td>{g.borrower_name}</td>
                    <td className="max-w-[16rem] truncate" title={g.purpose}>
                      {g.created_via === "api" && <span className="badge badge-xs badge-primary badge-outline mr-1">派發系統</span>}
                      {g.purpose || "-"}
                    </td>
                    <td><span className={`badge badge-sm ${st.badge}`}>{st.label}</span></td>
                    <td className="whitespace-nowrap text-xs">{rentalSpanText(g)}</td>
                    <td className="whitespace-nowrap text-xs opacity-60">{g.updated_at ? new Date(g.updated_at).toLocaleString() : "-"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <div className="modal-action items-center">
          <span className="text-sm text-base-content/60 mr-auto">已選 {selected.size} 筆</span>
          <button onClick={onClose} className="btn btn-ghost btn-sm">關閉</button>
          <button onClick={restore} disabled={selected.size === 0 || restoring} className="btn btn-primary btn-sm gap-1">
            {restoring ? <span className="loading loading-spinner loading-xs" /> : <ArchiveRestore size={14} />}
            還原所選
          </button>
        </div>
      </div>
      <form method="dialog" className="modal-backdrop"><button onClick={onClose}>close</button></form>
    </dialog>
  );
}
