import { useState } from "react";
import { useParams, useNavigate } from "react-router";
import apiClient from "../../lib/apiClient";
import { useDialog } from "../../components/DialogProvider";
import { MobileShell } from "../../components/mobile/MobileShell";
import { useRentalGroup } from "../../hooks/useRentalGroup";
import { minExtendDate } from "../../lib/dates";
import { enqueueAction, isNetworkError, usePendingActions, useOnlineStatus } from "../../lib/offlineQueue";

const todayStr = () => new Date().toISOString().slice(0, 10);

// Asking to extend a rental is exactly the kind of thing that comes up while
// away from the office (the trip runs long), so like the daily report and
// return pages it falls back to the offline queue instead of failing.
export function MobileExtend() {
  const { rentalId } = useParams<{ rentalId: string }>();
  const navigate = useNavigate();
  const dialog = useDialog();
  const online = useOnlineStatus();
  const pending = usePendingActions();
  const { group, loading, loadError } = useRentalGroup(rentalId);

  const [newDate, setNewDate] = useState("");
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (loading) {
    return <MobileShell title="申請續借" backTo="/m/rentals"><div className="flex justify-center py-12"><span className="loading loading-spinner"></span></div></MobileShell>;
  }
  if (loadError || !group) {
    return (
      <MobileShell title="申請續借" backTo="/m/rentals">
        <div className="text-center py-12 text-sm text-base-content/60">
          找不到這筆租借資料，請先在有網路連線時從「我的租借」開啟一次。
        </div>
      </MobileShell>
    );
  }

  const rentalFirstId = group.rentals[0].id;
  const minDate = minExtendDate(group.expected_return, todayStr());
  const date = newDate || minDate;
  const queued = pending.some((p) => p.type === "extend" && p.rentalId === rentalFirstId);
  const blocked = !!group.pending_extension || queued;
  const assetLabel = group.rentals.map((r) => r.asset_name).join("、");

  const handleSubmit = async () => {
    setSubmitting(true);
    try {
      const body = { new_expected_return: date, reason: reason.trim() };
      const queue = async () => {
        await enqueueAction({ type: "extend", rentalId: rentalFirstId, rentalNumber: group.rental_number, assetLabel, body });
        navigate("/m/rentals");
      };
      if (!navigator.onLine) { await queue(); return; }
      try {
        await apiClient.post(`/api/rentals/${rentalFirstId}/extend`, body);
        await dialog.success("續借申請已送出，待保管人／管理員審核後才會更新預計歸還日");
        navigate("/m/rentals");
      } catch (err) {
        if (isNetworkError(err)) { await queue(); return; }
        const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
        await dialog.error(resp?.error || "送出失敗");
      }
    } finally { setSubmitting(false); }
  };

  return (
    <MobileShell
      title="申請續借"
      backTo="/m/rentals"
      subtitle={`單號 ${group.rental_number}・${assetLabel}`}
      footer={
        <>
          {!online && !blocked && (
            <div className="text-xs text-warning-content bg-warning/10 rounded-lg px-3 py-2">
              目前離線，申請會先存在裝置上，回到連線環境（含公司 VPN）後自動送出
            </div>
          )}
          <button
            type="button"
            className={`btn btn-block ${online ? "btn-primary" : "btn-warning"}`}
            disabled={submitting || blocked || date < minDate || !reason.trim()}
            onClick={handleSubmit}
          >
            {submitting && <span className="loading loading-spinner loading-xs"></span>}
            {online ? "送出續借申請" : "儲存並排入同步佇列"}
          </button>
        </>
      }
    >
      {blocked && (
        <div className="text-sm rounded-lg bg-warning/10 px-3 py-2">
          {group.pending_extension
            ? `已有一筆續借申請審核中（續借至 ${group.pending_extension.requested_expected_return}），審核完成前不能再申請。`
            : "已有一筆續借申請存在裝置上，等待同步，同步完成前不能再申請。"}
        </div>
      )}

      <div className="text-sm text-base-content/70">
        目前預計歸還：<span className="font-mono">{group.expected_return || "-"}</span>。送出後需經保管人／管理員審核，核准前預計歸還日不會改變。
      </div>

      <div className="form-control">
        <label className="label"><span className="label-text text-sm">續借至</span></label>
        <input
          type="date"
          value={date}
          min={minDate}
          disabled={blocked}
          onChange={(e) => setNewDate(e.target.value)}
          className="input input-bordered"
        />
      </div>

      <div className="form-control">
        <label className="label"><span className="label-text text-sm">續借原因（必填）</span></label>
        <textarea
          value={reason}
          disabled={blocked}
          onChange={(e) => setReason(e.target.value)}
          placeholder="例如：出差行程延後"
          className="textarea textarea-bordered"
          rows={3}
        />
      </div>
    </MobileShell>
  );
}
