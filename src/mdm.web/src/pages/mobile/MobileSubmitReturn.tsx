import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router";
import apiClient from "../../lib/apiClient";
import { useDialog } from "../../components/DialogProvider";
import { MobileShell } from "../../components/mobile/MobileShell";
import { ChecklistFields } from "../../components/ChecklistFields";
import { useRentalGroup } from "../../hooks/useRentalGroup";
import { type ChecklistItem, type ChecklistAnswers, isChecklistItemFilled } from "../../lib/checklist";
import {
  enqueueAction, isNetworkError, uploadOrStagePhoto, resolvePhotoSrc, hasStagedPhotos, useOnlineStatus,
} from "../../lib/offlineQueue";

export function MobileSubmitReturn() {
  const { rentalId } = useParams<{ rentalId: string }>();
  const navigate = useNavigate();
  const dialog = useDialog();
  const online = useOnlineStatus();
  const { group, loading: groupLoading, loadError } = useRentalGroup(rentalId);

  const [items, setItems] = useState<ChecklistItem[]>([]);
  const [itemsLoading, setItemsLoading] = useState(true);
  const [values, setValues] = useState<ChecklistAnswers>({});
  const [notes, setNotes] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!group) return;
    const categoryIds = Array.from(new Set(group.rentals.map((r) => r.category_id).filter((v): v is string => !!v)));
    setItemsLoading(true);
    apiClient.get("/api/checklist-templates/resolve", { params: { category_ids: categoryIds.join(",") } })
      .then(({ data }) => setItems(data.items || []))
      .catch(() => setItems([]))
      .finally(() => setItemsLoading(false));
  }, [group]);

  const requiredFilled = items.every((item) => isChecklistItemFilled(item, values[item.key]));

  const handleSubmit = async () => {
    if (!group) return;
    setSubmitting(true);
    try {
      const body: Record<string, unknown> = { checklist: values, notes };
      const assetLabel = group.rentals.map((r) => r.asset_name).join("、");

      if (hasStagedPhotos(values) || !navigator.onLine) {
        await enqueueAction({ type: "submit-return", rentalId: group.rentals[0].id, rentalNumber: group.rental_number, assetLabel, body });
        navigate("/m/rentals");
        return;
      }
      try {
        await apiClient.post(`/api/rentals/${group.rentals[0].id}/submit-return`, body);
        navigate("/m/rentals");
      } catch (err) {
        if (isNetworkError(err)) {
          await enqueueAction({ type: "submit-return", rentalId: group.rentals[0].id, rentalNumber: group.rental_number, assetLabel, body });
          navigate("/m/rentals");
        } else {
          const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
          await dialog.error(resp?.error || "送出失敗");
        }
      }
    } finally { setSubmitting(false); }
  };

  if (groupLoading) {
    return <MobileShell title="我要歸還" backTo="/m/rentals"><div className="flex justify-center py-12"><span className="loading loading-spinner"></span></div></MobileShell>;
  }
  if (loadError || !group) {
    return (
      <MobileShell title="我要歸還" backTo="/m/rentals">
        <div className="text-center py-12 text-sm text-base-content/60">
          找不到這筆租借資料，請先在有網路連線時從「我的租借」開啟一次。
        </div>
      </MobileShell>
    );
  }

  return (
    <MobileShell
      title="我要歸還"
      backTo="/m/rentals"
      subtitle={`單號 ${group.rental_number}・${group.rentals.map((r) => r.asset_name).join("、")}`}
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
            disabled={submitting || !requiredFilled}
            onClick={handleSubmit}
          >
            {submitting && <span className="loading loading-spinner loading-xs"></span>}
            {online ? "送出歸還" : "儲存並排入同步佇列"}
          </button>
        </>
      }
    >
      {itemsLoading ? (
        <div className="flex justify-center py-8"><span className="loading loading-spinner"></span></div>
      ) : items.length === 0 ? (
        <p className="text-sm text-base-content/50 py-4 text-center">此分類沒有設定歸還清點項目</p>
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

      <div className="form-control">
        <label className="label"><span className="label-text text-sm">備註（選填）</span></label>
        <textarea
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder="記錄裝置狀況、損壞情形等"
          className="textarea textarea-bordered textarea-sm"
          rows={3}
        />
      </div>
    </MobileShell>
  );
}
