import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router";
import apiClient from "../../lib/apiClient";
import { useDialog } from "../../components/DialogProvider";
import { MobileShell } from "../../components/mobile/MobileShell";
import { ChecklistFields } from "../../components/ChecklistFields";
import { useRentalGroup } from "../../hooks/useRentalGroup";
import { type ChecklistItem, type ChecklistAnswers, isChecklistItemFilled } from "../../lib/checklist";
import { type DailyReport } from "../../lib/rentalTypes";
import {
  enqueueAction, isNetworkError, uploadOrStagePhoto, resolvePhotoSrc, hasStagedPhotos, useOnlineStatus,
} from "../../lib/offlineQueue";

const todayStr = () => new Date().toISOString().slice(0, 10);

export function MobileDailyReport() {
  const { rentalId } = useParams<{ rentalId: string }>();
  const navigate = useNavigate();
  const dialog = useDialog();
  const online = useOnlineStatus();
  const { group, loading: groupLoading, loadError } = useRentalGroup(rentalId);

  const [items, setItems] = useState<ChecklistItem[]>([]);
  const [itemsLoading, setItemsLoading] = useState(true);
  const [values, setValues] = useState<ChecklistAnswers>({});
  const [existingDates, setExistingDates] = useState<string[]>([]);
  const [reportDate, setReportDate] = useState(todayStr());
  const [backfillReason, setBackfillReason] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!group) return;
    const categoryIds = Array.from(new Set(group.rentals.map((r) => r.category_id).filter((v): v is string => !!v)));
    setItemsLoading(true);
    apiClient.get("/api/checklist-templates/resolve", { params: { category_ids: categoryIds.join(",") } })
      .then(({ data }) => setItems(data.items || []))
      .catch(() => setItems([]))
      .finally(() => setItemsLoading(false));

    apiClient.get(`/api/rentals/${group.rentals[0].id}/daily-reports`)
      .then(({ data }) => {
        const dates = ((data.reports as DailyReport[]) || []).map((r) => r.report_date);
        setExistingDates(dates);
        if (dates.includes(todayStr())) setReportDate("");
      })
      .catch(() => { /* best-effort — worst case the date picker just doesn't pre-exclude today */ });
  }, [group]);

  const isBackfill = reportDate !== "" && reportDate !== todayStr();
  const requiredFilled = items.every((item) => isChecklistItemFilled(item, values[item.key]));

  const handleSubmit = async () => {
    if (!group || !reportDate) return;
    setSubmitting(true);
    try {
      const body: Record<string, unknown> = { checklist: values };
      if (isBackfill) { body.report_date = reportDate; body.backfill_reason = backfillReason; }

      if (hasStagedPhotos(values) || !navigator.onLine) {
        await enqueueAction({ type: "daily-report", rentalId: group.rentals[0].id, rentalNumber: group.rental_number, assetLabel: group.rentals.map((r) => r.asset_name).join("、"), body });
        navigate("/m/rentals");
        return;
      }
      try {
        await apiClient.post(`/api/rentals/${group.rentals[0].id}/daily-report`, body);
        navigate("/m/rentals");
      } catch (err) {
        if (isNetworkError(err)) {
          await enqueueAction({ type: "daily-report", rentalId: group.rentals[0].id, rentalNumber: group.rental_number, assetLabel: group.rentals.map((r) => r.asset_name).join("、"), body });
          navigate("/m/rentals");
        } else {
          const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
          await dialog.error(resp?.error || "送出失敗");
        }
      }
    } finally { setSubmitting(false); }
  };

  if (groupLoading) {
    return <MobileShell title="今日回報" backTo="/m/rentals"><div className="flex justify-center py-12"><span className="loading loading-spinner"></span></div></MobileShell>;
  }
  if (loadError || !group) {
    return (
      <MobileShell title="今日回報" backTo="/m/rentals">
        <div className="text-center py-12 text-sm text-base-content/60">
          找不到這筆租借資料，請先在有網路連線時從「我的租借」開啟一次。
        </div>
      </MobileShell>
    );
  }

  return (
    <MobileShell
      title="今日回報"
      backTo="/m/rentals"
      subtitle={`單號 ${group.rental_number}・${group.rentals.map((r) => r.asset_name).join("、")}・${reportDate || todayStr()}${isBackfill ? "（補登）" : "（今天）"}`}
      footer={
        <>
          {!online && (
            <div className="text-xs text-warning-content bg-warning/10 rounded-lg px-3 py-2">
              目前離線，資料會先存在裝置上，回到連線環境（含公司 VPN）後自動送出
            </div>
          )}
          <button
            type="button"
            className={`btn btn-block ${online ? "btn-primary" : "btn-warning"}`}
            disabled={submitting || !reportDate || !requiredFilled || (isBackfill && !backfillReason.trim())}
            onClick={handleSubmit}
          >
            {submitting && <span className="loading loading-spinner loading-xs"></span>}
            {online ? "送出回報" : "儲存並排入同步佇列"}
          </button>
        </>
      }
    >
      <div className="form-control">
        <label className="label"><span className="label-text text-sm">回報日期</span></label>
        <select value={reportDate} onChange={(e) => setReportDate(e.target.value)} className="select select-bordered select-sm">
          {!existingDates.includes(todayStr()) && <option value={todayStr()}>今天（{todayStr()}）</option>}
          <option value="" disabled>— 或補登遺漏的一天 —</option>
          {group.borrow_date && (() => {
            const start = new Date(group.borrow_date);
            const end = group.expected_return ? new Date(group.expected_return) : new Date();
            const today = todayStr();
            const options: string[] = [];
            for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
              const s = d.toISOString().slice(0, 10);
              if (s < today && !existingDates.includes(s)) options.push(s);
            }
            return options.map((s) => <option key={s} value={s}>補登 {s}</option>);
          })()}
        </select>
      </div>

      {isBackfill && (
        <div className="form-control">
          <label className="label"><span className="label-text text-sm">補登原因（必填）</span></label>
          <textarea
            value={backfillReason}
            onChange={(e) => setBackfillReason(e.target.value)}
            placeholder="例如：忘記填，事後回想"
            className="textarea textarea-bordered textarea-sm"
            rows={2}
          />
        </div>
      )}

      {itemsLoading ? (
        <div className="flex justify-center py-8"><span className="loading loading-spinner"></span></div>
      ) : items.length === 0 ? (
        <p className="text-sm text-base-content/50 py-4 text-center">此分類沒有設定檢查清單項目</p>
      ) : (
        <ChecklistFields
          items={items}
          values={values}
          onChange={(key, v) => setValues((prev) => ({ ...prev, [key]: v }))}
          rentalNumber={group.rental_number}
          onUploadPhoto={(file, item, rentalNumber) => uploadOrStagePhoto(file, rentalNumber, item.key)}
          resolvePhotoSrc={resolvePhotoSrc}
        />
      )}
    </MobileShell>
  );
}
