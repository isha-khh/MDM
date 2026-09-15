import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Layers, X } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "./DialogProvider";
import { CategoryLeafSelect } from "./CategoryLeafSelect";

interface CategoryOption { id: string; parent_id: string | null; name: string; level: number; }
interface UserOption { id: string; username: string; display_name: string; }

interface BatchEditAssetsDialogProps {
  open: boolean;
  assetIds: string[];
  onClose: () => void;
  onDone: () => void;
}

// Fields this dialog can touch are intentionally limited to low-risk
// metadata (category/location/purpose/is_rentable via the generic batch
// endpoint) plus custodian (via the existing audited /api/assets-custody
// endpoint, called once per asset). Disposal/transfer already has its own
// batch flow (資產報廢申請), so it isn't duplicated here.
export function BatchEditAssetsDialog({ open, assetIds, onClose, onDone }: BatchEditAssetsDialogProps) {
  const { t } = useTranslation();
  const dialog = useDialog();

  const [categories, setCategories] = useState<CategoryOption[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const [applyCategory, setApplyCategory] = useState(false);
  const [categoryId, setCategoryId] = useState("");
  const [applyLocation, setApplyLocation] = useState(false);
  const [location, setLocation] = useState("");
  const [applyPurpose, setApplyPurpose] = useState(false);
  const [purpose, setPurpose] = useState("");
  const [applyRentable, setApplyRentable] = useState(false);
  const [isRentable, setIsRentable] = useState(true);
  const [applyCustodian, setApplyCustodian] = useState(false);
  const [custodianId, setCustodianId] = useState("");
  const [custodianReason, setCustodianReason] = useState("");

  useEffect(() => {
    if (!open) return;
    apiClient.get("/api/categories").then(({ data }) => setCategories(data.categories || [])).catch(() => {});
    apiClient.get("/api/users-list").then(({ data }) => setUsers(data.users || [])).catch(() => {});
    setApplyCategory(false); setCategoryId("");
    setApplyLocation(false); setLocation("");
    setApplyPurpose(false); setPurpose("");
    setApplyRentable(false); setIsRentable(true);
    setApplyCustodian(false); setCustodianId(""); setCustodianReason("");
  }, [open]);

  const anySelected = applyCategory || applyLocation || applyPurpose || applyRentable || applyCustodian;
  const custodianValid = !applyCustodian || (custodianId && custodianReason.trim());

  const handleSubmit = async () => {
    if (!anySelected) return;
    if (applyCustodian && !custodianValid) {
      await dialog.error(t("assets.batchEdit.errorCustodian"));
      return;
    }
    setSubmitting(true);
    try {
      const fields: Record<string, unknown> = {};
      if (applyCategory) fields.category_id = categoryId || null;
      if (applyLocation) fields.location = location;
      if (applyPurpose) fields.purpose = purpose;
      if (applyRentable) fields.is_rentable = isRentable;

      let updated = 0;
      const errors: string[] = [];

      if (Object.keys(fields).length > 0) {
        const { data } = await apiClient.post("/api/assets-batch-update", { ids: assetIds, fields });
        updated = data.updated ?? 0;
        if (Array.isArray(data.errors)) errors.push(...data.errors);
      }

      let custodianOk = 0;
      const custodianErrors: string[] = [];
      if (applyCustodian) {
        for (const id of assetIds) {
          try {
            await apiClient.post("/api/assets-custody", {
              action: "transfer", asset_id: id, to_user_id: custodianId, reason: custodianReason.trim(),
            });
            custodianOk++;
          } catch (err: unknown) {
            const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
            custodianErrors.push(`${id}: ${resp?.error || (err instanceof Error ? err.message : "")}`);
          }
        }
      }

      const lines = [
        Object.keys(fields).length > 0 ? t("assets.batchEdit.resultFields", { updated, total: assetIds.length }) : null,
        applyCustodian ? t("assets.batchEdit.resultCustodian", { updated: custodianOk, total: assetIds.length }) : null,
      ].filter(Boolean) as string[];
      const allErrors = [...errors, ...custodianErrors];
      if (allErrors.length > 0) {
        await dialog.error(lines.join("\n"), allErrors);
      } else {
        await dialog.success(lines.join("\n"));
      }
      onDone();
      onClose();
    } catch (err: unknown) {
      const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
      await dialog.error(resp?.error || (err instanceof Error ? err.message : t("assets.batchEdit.errorSubmit")));
    } finally {
      setSubmitting(false);
    }
  };

  if (!open) return null;

  return (
    <dialog className="modal modal-open">
      <div className="modal-box max-w-lg">
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-bold text-lg flex items-center gap-2">
            <Layers size={18} />
            {t("assets.batchEdit.title", { count: assetIds.length })}
          </h3>
          <button onClick={onClose} className="btn btn-ghost btn-sm btn-circle"><X size={16} /></button>
        </div>
        <p className="text-sm text-base-content/60 mb-4">{t("assets.batchEdit.hint")}</p>

        <div className="space-y-3">
          <div className="flex items-start gap-3">
            <input type="checkbox" className="checkbox checkbox-sm mt-1" checked={applyCategory} onChange={(e) => setApplyCategory(e.target.checked)} />
            <div className="flex-1">
              <label className="label-text font-medium">{t("assets.category")}</label>
              <CategoryLeafSelect
                categories={categories}
                value={categoryId}
                onChange={setCategoryId}
                disabled={!applyCategory}
              />
            </div>
          </div>

          <div className="flex items-start gap-3">
            <input type="checkbox" className="checkbox checkbox-sm mt-1" checked={applyLocation} onChange={(e) => setApplyLocation(e.target.checked)} />
            <div className="flex-1">
              <label className="label-text font-medium">{t("assets.location")}</label>
              <input
                type="text" value={location} onChange={(e) => setLocation(e.target.value)}
                disabled={!applyLocation} className="input input-bordered input-sm w-full"
              />
            </div>
          </div>

          <div className="flex items-start gap-3">
            <input type="checkbox" className="checkbox checkbox-sm mt-1" checked={applyPurpose} onChange={(e) => setApplyPurpose(e.target.checked)} />
            <div className="flex-1">
              <label className="label-text font-medium">{t("assets.purpose")}</label>
              <input
                type="text" value={purpose} onChange={(e) => setPurpose(e.target.value)}
                disabled={!applyPurpose} className="input input-bordered input-sm w-full"
              />
            </div>
          </div>

          <div className="flex items-start gap-3">
            <input type="checkbox" className="checkbox checkbox-sm mt-1" checked={applyRentable} onChange={(e) => setApplyRentable(e.target.checked)} />
            <div className="flex-1">
              <label className="label-text font-medium">{t("assets.isRentable")}</label>
              <select
                value={isRentable ? "true" : "false"} onChange={(e) => setIsRentable(e.target.value === "true")}
                disabled={!applyRentable} className="select select-bordered select-sm w-full"
              >
                <option value="true">{t("common.yes")}</option>
                <option value="false">{t("common.no")}</option>
              </select>
            </div>
          </div>

          <div className="divider my-1"></div>

          <div className="flex items-start gap-3">
            <input type="checkbox" className="checkbox checkbox-sm mt-1" checked={applyCustodian} onChange={(e) => setApplyCustodian(e.target.checked)} />
            <div className="flex-1 space-y-2">
              <label className="label-text font-medium">{t("assets.custodian")}</label>
              <select
                value={custodianId} onChange={(e) => setCustodianId(e.target.value)}
                disabled={!applyCustodian} className="select select-bordered select-sm w-full"
              >
                <option value="">{t("custody.selectUser")}</option>
                {users.map((u) => <option key={u.id} value={u.id}>{u.display_name || u.username}</option>)}
              </select>
              <input
                type="text" value={custodianReason} onChange={(e) => setCustodianReason(e.target.value)}
                placeholder={t("custody.reasonPlaceholder")}
                disabled={!applyCustodian} className="input input-bordered input-sm w-full"
              />
              <p className="text-xs text-base-content/50">{t("assets.batchEdit.custodianHint")}</p>
            </div>
          </div>
        </div>

        <div className="modal-action">
          <button onClick={onClose} className="btn btn-ghost btn-sm">{t("common.cancel")}</button>
          <button
            onClick={handleSubmit}
            disabled={!anySelected || submitting || (applyCustodian && !custodianValid)}
            className="btn btn-primary btn-sm gap-1"
          >
            {submitting && <span className="loading loading-spinner loading-xs"></span>}
            {t("assets.batchEdit.apply", { count: assetIds.length })}
          </button>
        </div>
      </div>
      <form method="dialog" className="modal-backdrop"><button type="button" onClick={onClose}>close</button></form>
    </dialog>
  );
}
