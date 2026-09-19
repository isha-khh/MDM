import { useState, useEffect, useMemo } from "react";
import { useAuthStore } from "../stores/authStore";
import { useTranslation } from "react-i18next";
import { AssetPicker } from "../components/AssetPicker";
import { CategoryLeafSelect } from "../components/CategoryLeafSelect";
import apiClient from "../lib/apiClient";
import { useDialog } from "../components/DialogProvider";
import {
  Check, X, RotateCcw, Play, UserPlus, Clock,
  CheckCircle, AlertCircle, ArrowRight, FileDown, Archive, Plus, Trash2, Gauge,
  MapPin, Camera,
} from "lucide-react";
import type { ColDef, ICellRendererParams } from "ag-grid-enterprise";
import { DataGrid } from "../components/DataGrid";

// Dynamic checklist item, resolved from the category-bound templates
// maintained in Categories.tsx (Phase 2a). Mirrors the backend's
// domain.ChecklistItem.
type ChecklistItemType = "boolean" | "text" | "number" | "location" | "photo";

interface ChecklistItem {
  key: string;
  label: string;
  type: ChecklistItemType;
  required: boolean;
  unit?: string;
  maxCount?: number;
}

type ChecklistAnswers = Record<string, unknown>;

interface Rental {
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
  daily_tracking_required?: boolean;
}

interface RentalGroup {
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
  daily_tracking_required?: boolean;
}

interface DailyReport {
  id: string;
  report_date: string;
  checklist: Record<string, unknown>;
  backfill_reason: string;
  reported_at: string;
}

interface UserOption {
  id: string;
  username: string;
  display_name: string;
}

const statusConfig: Record<string, { label: string; badge: string; icon: React.ReactNode }> = {
  pending:        { label: "待核准", badge: "badge-warning", icon: <Clock size={14} /> },
  approved:       { label: "已核准", badge: "badge-info",    icon: <Check size={14} /> },
  active:         { label: "借出中", badge: "badge-success", icon: <Play size={14} /> },
  pending_return: { label: "待核對", badge: "badge-info",    icon: <Clock size={14} /> },
  returned:       { label: "已歸還", badge: "badge-ghost",   icon: <RotateCcw size={14} /> },
  rejected:       { label: "已拒絕", badge: "badge-error",   icon: <X size={14} /> },
};

function groupByRentalNumber(rentals: Rental[]): RentalGroup[] {
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
      // Union across the batch, matching the backend's own union policy for
      // "does this batch touch any daily-tracking category".
      daily_tracking_required: items.some((it) => it.daily_tracking_required),
    });
  }
  groups.sort((a, b) => b.rental_number - a.rental_number);
  return groups;
}

async function downloadExportExcel(ids?: string[]) {
  const params = new URLSearchParams();
  if (ids && ids.length > 0) {
    params.set("ids", ids.join(","));
  }
  const resp = await apiClient.get(`/api/rentals-export?${params}`, {
    responseType: "blob",
  });
  const url = URL.createObjectURL(resp.data);
  const a = document.createElement("a");
  a.href = url;
  const disposition = resp.headers["content-disposition"] || "";
  const match = disposition.match(/filename="?([^"]+)"?/);
  a.download = match?.[1] || `租借記錄_${new Date().toISOString().slice(0, 10)}.xlsx`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

function isChecklistItemFilled(item: ChecklistItem, value: unknown): boolean {
  if (!item.required) return true;
  switch (item.type) {
    case "boolean": return value === true;
    case "photo": return Array.isArray(value) && value.length > 0;
    case "location": return !!value && typeof value === "object";
    case "number": return value !== undefined && value !== null && value !== "";
    case "text": default: return typeof value === "string" && value.trim() !== "";
  }
}

// Downscales an image client-side before upload (long edge capped at
// maxDim, re-encoded as JPEG) — a phone camera photo straight off the
// sensor can be 5-10MB, which would otherwise land directly in the
// database via checklist_photos. Falls back to the original file if
// anything about the canvas path fails.
function resizeImage(file: File, maxDim: number): Promise<Blob> {
  return new Promise((resolve) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      let { width, height } = img;
      if (width > maxDim || height > maxDim) {
        const scale = maxDim / Math.max(width, height);
        width = Math.round(width * scale);
        height = Math.round(height * scale);
      }
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (!ctx) { resolve(file); return; }
      ctx.drawImage(img, 0, 0, width, height);
      canvas.toBlob((blob) => resolve(blob || file), "image/jpeg", 0.85);
    };
    img.onerror = () => { URL.revokeObjectURL(url); resolve(file); };
    img.src = url;
  });
}

function ChecklistLocationField({ item, value, onChange }: {
  item: ChecklistItem;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const [manual, setManual] = useState(false);
  const [busy, setBusy] = useState(false);
  const v = value as { lat?: number; lng?: number; address?: string } | undefined;

  const capture = () => {
    setBusy(true);
    if (!navigator.geolocation) { setManual(true); setBusy(false); return; }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        onChange({ lat: pos.coords.latitude, lng: pos.coords.longitude, accuracy: pos.coords.accuracy, capturedAt: new Date().toISOString() });
        setBusy(false);
      },
      () => { setManual(true); setBusy(false); },
      { enableHighAccuracy: true, timeout: 10000 },
    );
  };

  return (
    <div className="form-control">
      <label className="label">
        <span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
      </label>
      {v?.lat != null ? (
        <div className="flex items-center gap-2 text-sm flex-wrap">
          <MapPin size={14} className="text-success" />
          <a className="link link-primary" target="_blank" rel="noreferrer" href={`https://maps.google.com/?q=${v.lat},${v.lng}`}>
            已定位（開啟地圖）
          </a>
          <button type="button" className="btn btn-ghost btn-xs" onClick={capture}>重新定位</button>
        </div>
      ) : manual ? (
        <input
          type="text"
          className="input input-bordered input-sm"
          placeholder="手動輸入地址"
          value={v?.address || ""}
          onChange={(e) => onChange({ address: e.target.value })}
        />
      ) : (
        <div className="flex items-center gap-2">
          <button type="button" className="btn btn-outline btn-sm gap-1" disabled={busy} onClick={capture}>
            {busy ? <span className="loading loading-spinner loading-xs" /> : <MapPin size={14} />} 取得目前位置
          </button>
          <button type="button" className="btn btn-link btn-xs" onClick={() => setManual(true)}>改用手動輸入地址</button>
        </div>
      )}
    </div>
  );
}

function ChecklistPhotoField({ item, rentalNumber, value, onChange }: {
  item: ChecklistItem;
  rentalNumber: number;
  value: unknown;
  onChange: (v: string[]) => void;
}) {
  const [uploading, setUploading] = useState(false);
  const ids = Array.isArray(value) ? (value as string[]) : [];
  const max = item.maxCount || 0;

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setUploading(true);
    try {
      const resized = await resizeImage(file, 1600);
      const form = new FormData();
      form.append("file", resized, file.name || "photo.jpg");
      form.append("rental_number", String(rentalNumber));
      form.append("item_key", item.key);
      const { data } = await apiClient.post("/api/checklist-photos", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      onChange([...ids, data.id]);
    } catch { /* best-effort — user can just try uploading again */ }
    finally { setUploading(false); }
  };

  return (
    <div className="form-control">
      <label className="label">
        <span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
      </label>
      <div className="flex flex-wrap gap-2 items-center">
        {ids.map((id) => (
          <div key={id} className="relative">
            <img src={`/api/checklist-photos/${id}`} alt="" className="w-16 h-16 object-cover rounded border border-base-300" />
            <button
              type="button"
              className="btn btn-error btn-xs btn-circle absolute -top-2 -right-2"
              onClick={() => onChange(ids.filter((existing) => existing !== id))}
            >
              <X size={10} />
            </button>
          </div>
        ))}
        {(max === 0 || ids.length < max) && (
          <label className="btn btn-outline btn-sm gap-1 cursor-pointer">
            {uploading ? <span className="loading loading-spinner loading-xs" /> : <Camera size={14} />} 拍照/上傳
            <input type="file" accept="image/*" capture="environment" className="hidden" onChange={handleFile} disabled={uploading} />
          </label>
        )}
      </div>
    </div>
  );
}

export function Rentals() {
  const { t } = useTranslation();
  const { user } = useAuthStore();
  const dialog = useDialog();
  const [rentals, setRentals] = useState<Rental[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [categories, setCategories] = useState<{ id: string; parent_id: string | null; name: string; level: number }[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [createTab, setCreateTab] = useState<"asset" | "category">("asset");
  const [statusFilter, setStatusFilter] = useState("");
  const [showArchived, setShowArchived] = useState(false);

  // Selection — by rental_number
  const [selectedNumbers, setSelectedNumbers] = useState<Set<number>>(new Set());

  // Create form — shared
  const todayStr = () => new Date().toISOString().slice(0, 10);
  const [borrowerId, setBorrowerId] = useState("");
  const [purpose, setPurpose] = useState("");
  const [borrowDate, setBorrowDate] = useState(todayStr());
  const [expectedReturn, setExpectedReturn] = useState("");
  const [multiDayReason, setMultiDayReason] = useState("");
  const [notes, setNotes] = useState("");
  const [creating, setCreating] = useState(false);
  // Asset mode
  const [selectedAssets, setSelectedAssets] = useState<string[]>([]);
  // Category mode — multiple lines
  const [catLines, setCatLines] = useState<{ categoryId: string; quantity: number }[]>([{ categoryId: "", quantity: 1 }]);

  const groups = useMemo(() => groupByRentalNumber(rentals), [rentals]);

  const loadRentals = async () => {
    setLoading(true);
    try {
      const { data } = await apiClient.get("/api/rentals", {
        params: { status: statusFilter, show_archived: showArchived ? "true" : "" },
      });
      setRentals(data.rentals || []);
      setSelectedNumbers(new Set());
    } catch (err) { console.error("Load rentals:", err); }
    finally { setLoading(false); }
  };

  const loadUsers = async () => {
    try {
      const { data } = await apiClient.get("/api/users-list");
      setUsers(data.users || []);
    } catch { /* */ }
  };

  const loadCategories = async () => {
    try {
      const { data } = await apiClient.get("/api/categories");
      setCategories(data.categories || []);
    } catch { /* */ }
  };

  useEffect(() => { loadRentals(); }, [statusFilter, showArchived]);
  useEffect(() => { loadUsers(); loadCategories(); }, []);

  const resetCreateForm = () => {
    setSelectedAssets([]);
    setBorrowerId("");
    setPurpose("");
    setBorrowDate(todayStr());
    setExpectedReturn("");
    setMultiDayReason("");
    setNotes("");
    setCatLines([{ categoryId: "", quantity: 1 }]);
  };

  // Multi-day rentals (borrow_date ≠ expected_return) may require a reason —
  // enforced server-side only for "逐日追蹤" categories (e.g. vehicles), but
  // shown proactively here so the user isn't surprised by a rejected submit.
  const isMultiDay = !!expectedReturn && expectedReturn !== borrowDate;

  const submitCreate = async () => {
    setCreating(true);
    try {
      const common = {
        borrower_id: borrowerId,
        purpose,
        borrow_date: borrowDate || null,
        expected_return: expectedReturn || null,
        multi_day_reason: multiDayReason,
        notes,
      };
      if (createTab === "asset") {
        if (!borrowerId || selectedAssets.length === 0) return;
        await apiClient.post("/api/rentals", { asset_ids: selectedAssets, ...common });
      } else {
        const validLines = catLines.filter((l) => l.categoryId && l.quantity >= 1);
        if (!borrowerId || validLines.length === 0) return;
        await apiClient.post("/api/rentals", {
          category_lines: validLines.map((l) => ({ category_id: l.categoryId, quantity: l.quantity })),
          ...common,
        });
      }
      setShowCreate(false);
      resetCreateForm();
      loadRentals();
    } catch (err: unknown) {
      const resp = (err as { response?: { data?: { error?: string; devices?: string[] } } })?.response?.data;
      await dialog.error(resp?.error || (err instanceof Error ? err.message : "建立失敗"), resp?.devices || []);
    } finally { setCreating(false); }
  };

  // 分類注意事項（Phase 3）——送出申請前先解析這批資產涉及的分類是否有尚未
  // 同意過的注意事項，有的話攔截提交、跳出彙整 dialog，全部勾選同意後才真的
  // 呼叫 submitCreate() 並記錄同意紀錄。
  const [noticePending, setNoticePending] = useState<{ category_id: string; content: string }[]>([]);
  const [noticeAgreed, setNoticeAgreed] = useState<Record<string, boolean>>({});
  const [noticeChecking, setNoticeChecking] = useState(false);
  const [noticeSubmitting, setNoticeSubmitting] = useState(false);

  const resolveCreateCategoryIds = async (): Promise<string[]> => {
    if (createTab === "category") {
      return Array.from(new Set(catLines.map((l) => l.categoryId).filter((v): v is string => !!v)));
    }
    try {
      const { data } = await apiClient.get("/api/rental-pickable-assets");
      const byId = new Map<string, string | null>(
        (data.assets || []).map((a: { asset_id: string; category_id: string | null }) => [a.asset_id, a.category_id]),
      );
      return Array.from(new Set(selectedAssets.map((id) => byId.get(id)).filter((v): v is string => !!v)));
    } catch { return []; }
  };

  const handleCreateClick = async () => {
    setNoticeChecking(true);
    try {
      const categoryIds = await resolveCreateCategoryIds();
      if (categoryIds.length === 0) { await submitCreate(); return; }
      const { data } = await apiClient.get("/api/category-notices/resolve", {
        params: { category_ids: categoryIds.join(",") },
      });
      const pending: { category_id: string; content: string }[] = data.pending || [];
      if (pending.length === 0) {
        await submitCreate();
      } else {
        setNoticePending(pending);
        setNoticeAgreed({});
      }
    } catch {
      await submitCreate();
    } finally { setNoticeChecking(false); }
  };

  const allNoticesAgreed = noticePending.every((n) => noticeAgreed[n.category_id]);

  const confirmNoticesAndCreate = async () => {
    setNoticeSubmitting(true);
    try {
      await apiClient.post("/api/category-notice-acks", { category_ids: noticePending.map((n) => n.category_id) });
      setNoticePending([]);
      await submitCreate();
    } catch (err: unknown) {
      const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
      await dialog.error(resp?.error || "同意紀錄寫入失敗");
    } finally { setNoticeSubmitting(false); }
  };

  // Return dialog state — checklist items are resolved dynamically per the
  // batch's asset categories (Phase 2a). Phase 2b splits the actual submit
  // into two stages sharing this one dialog shell: "submit" is the borrower
  // (or admin) reporting while the devices are still with them, "verify" is
  // the custodian (or admin) confirming/correcting that report before the
  // devices are marked returned.
  const [returnGroup, setReturnGroup] = useState<RentalGroup | null>(null);
  const [returnStage, setReturnStage] = useState<"submit" | "verify">("submit");
  const [returnItems, setReturnItems] = useState<ChecklistItem[]>([]);
  const [returnItemsLoading, setReturnItemsLoading] = useState(false);
  const [returnValues, setReturnValues] = useState<ChecklistAnswers>({});
  const [returnNotes, setReturnNotes] = useState("");
  const [crossDayReason, setCrossDayReason] = useState("");

  const allRequiredFilled = returnItems.every((item) => isChecklistItemFilled(item, returnValues[item.key]));
  // Only relevant at the verify stage: a daily-tracking rental that's being
  // verified after its expected_return date needs an overrun explanation —
  // mirrors the backend's own check, just surfaced before submit instead of
  // rejected after.
  const isOverdueVerify = returnStage === "verify" && !!returnGroup?.daily_tracking_required
    && !!returnGroup?.expected_return && todayStr() > returnGroup.expected_return;

  // Read-only history shown alongside the verify-stage dialog for
  // daily-tracking rentals, so the custodian can sanity-check the reported
  // checklist against each day's individual entry before confirming.
  const [verifyDailyReports, setVerifyDailyReports] = useState<DailyReport[]>([]);

  const openReturnDialog = async (group: RentalGroup, stage: "submit" | "verify") => {
    setReturnGroup(group);
    setReturnStage(stage);
    setReturnValues(stage === "verify" ? (group.return_checklist_reported || {}) : {});
    setReturnNotes("");
    setCrossDayReason("");
    setVerifyDailyReports([]);
    setReturnItemsLoading(true);
    try {
      const categoryIds = Array.from(new Set(group.rentals.map((rl) => rl.category_id).filter((v): v is string => !!v)));
      const { data } = await apiClient.get("/api/checklist-templates/resolve", {
        params: { category_ids: categoryIds.join(",") },
      });
      setReturnItems(data.items || []);
    } catch {
      setReturnItems([]);
    } finally { setReturnItemsLoading(false); }

    if (stage === "verify" && group.daily_tracking_required) {
      try {
        const { data } = await apiClient.get(`/api/rentals/${group.rentals[0].id}/daily-reports`);
        setVerifyDailyReports(data.reports || []);
      } catch { /* best-effort, not required to complete verification */ }
    }
  };

  // Daily report dialog (逐日追蹤分類：每日回報，跟歸還是分開的動作)
  const [dailyReportGroup, setDailyReportGroup] = useState<RentalGroup | null>(null);
  const [dailyReportDate, setDailyReportDate] = useState("");
  const [dailyReportMileage, setDailyReportMileage] = useState("");
  const [dailyReportBackfillReason, setDailyReportBackfillReason] = useState("");
  const [dailyReportExistingDates, setDailyReportExistingDates] = useState<string[]>([]);
  const [dailyReportSubmitting, setDailyReportSubmitting] = useState(false);

  const isDailyReportBackfill = dailyReportDate !== "" && dailyReportDate !== todayStr();

  const openDailyReport = async (group: RentalGroup) => {
    const rentalId = group.rentals[0].id;
    let existingDates: string[] = [];
    try {
      const { data } = await apiClient.get(`/api/rentals/${rentalId}/daily-reports`);
      existingDates = (data.reports as DailyReport[] || []).map((r) => r.report_date);
    } catch { /* best-effort — an empty list just means no prior reports fetched */ }
    setDailyReportExistingDates(existingDates);
    setDailyReportGroup(group);
    setDailyReportMileage("");
    setDailyReportBackfillReason("");
    // Default to today unless it's already covered, in which case default to
    // blank so the user has to deliberately pick a missed day to backfill.
    setDailyReportDate(existingDates.includes(todayStr()) ? "" : todayStr());
  };

  const confirmDailyReport = async () => {
    if (!dailyReportGroup || !dailyReportDate) return;
    setDailyReportSubmitting(true);
    try {
      const body: Record<string, unknown> = { checklist: { mileage: dailyReportMileage ? Number(dailyReportMileage) : null } };
      if (isDailyReportBackfill) {
        body.report_date = dailyReportDate;
        body.backfill_reason = dailyReportBackfillReason;
      }
      await apiClient.post(`/api/rentals/${dailyReportGroup.rentals[0].id}/daily-report`, body);
      setDailyReportGroup(null);
      loadRentals();
    } catch (err: unknown) {
      const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
      await dialog.error(resp?.error || (err instanceof Error ? err.message : "回報失敗"));
    } finally { setDailyReportSubmitting(false); }
  };

  const doAction = async (rentalId: string, action: string) => {
    const labels: Record<string, string> = {
      approve: "核准此租借申請（整批）？",
      activate: "確認借出裝置（整批）？",
      reject: "拒絕此租借申請（整批）？",
    };
    if (!(await dialog.confirm(labels[action] || `${action}?`))) return;
    try {
      await apiClient.post(`/api/rentals/${rentalId}/${action}`);
      loadRentals();
    } catch (err) {
      await dialog.error("操作失敗: " + (err instanceof Error ? err.message : ""));
    }
  };

  const confirmReturn = async () => {
    if (!returnGroup) return;
    try {
      if (returnStage === "submit") {
        await apiClient.post(`/api/rentals/${returnGroup.rentals[0].id}/submit-return`, {
          notes: returnNotes,
          checklist: returnValues,
        });
      } else {
        await apiClient.post(`/api/rentals/${returnGroup.rentals[0].id}/return`, {
          notes: returnNotes,
          checklist: returnValues,
          cross_day_reason: crossDayReason,
        });
      }
      setReturnGroup(null);
      loadRentals();
    } catch (err: unknown) {
      const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
      await dialog.error(resp?.error || "歸還失敗: " + (err instanceof Error ? err.message : ""));
    }
  };

  const isAdmin = user?.role === "admin";
  const isViewer = user?.role === "viewer";
  const canExport = user?.role === "admin" || user?.role === "operator";

  useEffect(() => {
    if (isViewer && user?.id && !borrowerId) {
      setBorrowerId(user.id);
    }
  }, [isViewer, user]);

  const canApprove = (group: RentalGroup) => {
    if (isAdmin) return true;
    if (group.custodian_id && group.custodian_id === user?.id) return true;
    return false;
  };

  // Selection helpers
  const toggleSelect = (num: number) => {
    setSelectedNumbers((prev) => {
      const next = new Set(prev);
      if (next.has(num)) next.delete(num); else next.add(num);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (selectedNumbers.size === groups.length) {
      setSelectedNumbers(new Set());
    } else {
      setSelectedNumbers(new Set(groups.map((g) => g.rental_number)));
    }
  };

  const selectedGroups = useMemo(
    () => groups.filter((g) => selectedNumbers.has(g.rental_number)),
    [groups, selectedNumbers],
  );

  // Export
  const handleExport = async () => {
    const target = selectedGroups.length > 0 ? selectedGroups : groups;
    if (target.length === 0) return;
    const ids = target.flatMap((g) => g.rentals.map((r) => r.id));
    await downloadExportExcel(ids);
  };

  const columnDefs = useMemo<ColDef<RentalGroup>[]>(() => {
    const defs: ColDef<RentalGroup>[] = [];
    if (canExport) {
      defs.push({
        headerName: "",
        colId: "select",
        width: 44,
        pinned: "left",
        sortable: false,
        filter: false,
        resizable: false,
        headerComponent: () => (
          <input
            type="checkbox"
            className="checkbox checkbox-xs"
            checked={groups.length > 0 && selectedNumbers.size === groups.length}
            onChange={toggleSelectAll}
          />
        ),
        cellRenderer: (p: ICellRendererParams<RentalGroup>) => (
          <input
            type="checkbox"
            className="checkbox checkbox-xs"
            checked={selectedNumbers.has(p.data!.rental_number)}
            onChange={() => toggleSelect(p.data!.rental_number)}
          />
        ),
      });
    }
    defs.push({
      headerName: "",
      colId: "expand",
      width: 44,
      sortable: false,
      filter: false,
      resizable: false,
      cellRenderer: "agGroupCellRenderer",
      cellRendererParams: { suppressCount: true },
    });
    defs.push({
      headerName: "單號",
      field: "rental_number",
      width: 110,
      cellRenderer: (p: ICellRendererParams<RentalGroup>) => (
        <span className="font-mono text-sm font-medium">
          {p.value}
          {p.data!.is_archived && <span className="badge badge-xs badge-ghost ml-1">存查</span>}
        </span>
      ),
    });
    defs.push({
      headerName: "資產",
      colId: "device",
      minWidth: 200,
      valueGetter: (p) => {
        const r = p.data?.rentals[0];
        return r?.device_name || r?.asset_name || r?.device_serial || r?.asset_number || "";
      },
      cellRenderer: (p: ICellRendererParams<RentalGroup>) => {
        const first = p.data!.rentals[0];
        const primary = first.device_name || first.asset_name || first.device_serial || first.asset_number || "-";
        const secondary = first.device_serial || first.asset_number;
        return p.data!.rentals.length > 1 ? (
          <div>
            <span className="font-medium">{primary}</span>
            <span className="badge badge-sm badge-outline ml-1">共 {p.data!.rentals.length} 件</span>
          </div>
        ) : (
          <div>
            <div className="font-medium">{primary}{!first.device_udid && <span className="badge badge-xs badge-outline ml-1">獨立</span>}</div>
            <div className="text-xs opacity-50 font-mono">{secondary}</div>
          </div>
        );
      },
    });
    defs.push({ headerName: "借用人", field: "borrower_name", width: 120, cellClass: "font-medium" });
    defs.push({ headerName: "保管人", field: "custodian_name", width: 120, cellClass: "text-sm opacity-70", valueFormatter: (p) => p.value || "-" });
    defs.push({ headerName: "用途", field: "purpose", minWidth: 140, cellClass: "text-sm", valueFormatter: (p) => p.value || "-" });
    defs.push({
      headerName: "狀態",
      field: "status",
      width: 120,
      cellRenderer: (p: ICellRendererParams<RentalGroup>) => {
        const sc = statusConfig[p.value as string] || statusConfig.pending;
        return <span className={`badge badge-sm gap-1 ${sc.badge}`}>{sc.icon} {sc.label}</span>;
      },
    });
    defs.push({
      headerName: "借出日期",
      field: "borrow_date",
      width: 120,
      cellClass: "text-sm opacity-70",
      valueFormatter: (p) => p.value ? new Date(p.value as string).toLocaleDateString() : "-",
    });
    defs.push({ headerName: "預計歸還", field: "expected_return", width: 120, cellClass: "text-sm opacity-70", valueFormatter: (p) => p.value || "-" });
    defs.push({ headerName: "核准人", field: "approver_name", width: 120, cellClass: "text-sm", valueFormatter: (p) => p.value || "-" });
    defs.push({
      headerName: "操作",
      colId: "actions",
      width: 180,
      pinned: "right",
      sortable: false,
      filter: false,
      cellRenderer: (p: ICellRendererParams<RentalGroup>) => {
        const g = p.data!;
        const firstRentalId = g.rentals[0].id;
        return (
          <div className="flex gap-1 h-full items-center">
            {g.status === "pending" && canApprove(g) && (
              <>
                <button onClick={() => doAction(firstRentalId, "approve")} className="btn btn-success btn-xs gap-1"><CheckCircle size={12} /> 核准</button>
                <button onClick={() => doAction(firstRentalId, "reject")} className="btn btn-error btn-xs gap-1"><AlertCircle size={12} /> 拒絕</button>
              </>
            )}
            {g.status === "approved" && isAdmin && (
              <button onClick={() => doAction(firstRentalId, "activate")} className="btn btn-primary btn-xs gap-1"><Play size={12} /> 借出</button>
            )}
            {g.status === "active" && (isAdmin || g.borrower_id === user?.id) && (
              <button onClick={() => openReturnDialog(g, "submit")} className="btn btn-warning btn-xs gap-1"><RotateCcw size={12} /> 我要歸還</button>
            )}
            {g.status === "pending_return" && canApprove(g) && (
              <button onClick={() => openReturnDialog(g, "verify")} className="btn btn-info btn-xs gap-1"><CheckCircle size={12} /> 核對歸還</button>
            )}
            {g.status === "active" && g.daily_tracking_required && (isAdmin || g.borrower_id === user?.id) && (
              <button onClick={() => openDailyReport(g)} className="btn btn-outline btn-xs gap-1"><Gauge size={12} /> 每日回報</button>
            )}
          </div>
        );
      },
    });
    return defs;
  }, [canExport, selectedNumbers, groups.length, isAdmin, user]);

  const detailCellRendererParams = useMemo(() => ({
    detailGridOptions: {
      columnDefs: [
        { headerName: "#", valueGetter: (p: any) => (p.node?.rowIndex ?? 0) + 1, width: 60 },
        {
          headerName: "名稱",
          flex: 1,
          valueGetter: (p: any) => p.data?.device_name || p.data?.asset_name || p.data?.device_serial || p.data?.asset_number || "-",
        },
        {
          headerName: "序號/財產編號",
          flex: 1,
          cellClass: "font-mono text-xs",
          valueGetter: (p: any) => p.data?.device_serial || p.data?.asset_number || "-",
        },
      ] as ColDef<Rental>[],
      defaultColDef: { sortable: false, filter: false, resizable: true },
      domLayout: "autoHeight" as const,
      headerHeight: 32,
      rowHeight: 32,
    },
    getDetailRowData: (p: any) => { p.successCallback((p.data as RentalGroup).rentals); },
  }), []);

  // Export + archive
  const handleExportAndArchive = async () => {
    if (selectedGroups.length === 0) {
      await dialog.alert("請先勾選要存查的記錄");
      return;
    }
    const allIds = selectedGroups.flatMap((g) => g.rentals.map((r) => r.id));
    await downloadExportExcel(allIds);

    try {
      await apiClient.post("/api/rentals-archive", { ids: allIds });
      loadRentals();
    } catch (err) {
      await dialog.error("標記存查失敗: " + (err instanceof Error ? err.message : ""));
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">租借管理</h1>
          <p className="text-sm text-base-content/60">裝置借出、歸還與追蹤</p>
        </div>
        <div className="flex gap-2 flex-wrap">
          <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="select select-bordered select-sm" data-tour="rental-filter">
            <option value="">全部狀態</option>
            <option value="pending">待核准</option>
            <option value="approved">已核准</option>
            <option value="active">借出中</option>
            <option value="pending_return">待核對</option>
            <option value="returned">已歸還</option>
            <option value="rejected">已拒絕</option>
          </select>
          <label className="flex items-center gap-1.5 cursor-pointer text-sm">
            <input
              type="checkbox"
              className="checkbox checkbox-xs"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
            />
            顯示存查
          </label>
          <button onClick={() => setShowCreate(true)} className="btn btn-primary btn-sm gap-1" data-tour="rental-create">
            <UserPlus size={14} /> 新增租借
          </button>
        </div>
      </div>

      {/* Create form */}
      {showCreate && (
        <div className="card bg-base-100 shadow">
          <div className="card-body">
            <div className="flex items-center justify-between mb-2">
              <h2 className="card-title text-base">新增租借申請</h2>
              <button onClick={() => { setShowCreate(false); resetCreateForm(); }} className="btn btn-ghost btn-sm btn-circle"><X size={16} /></button>
            </div>

            {/* Tabs */}
            <div role="tablist" className="tabs tabs-bordered mb-4">
              <button
                role="tab"
                className={`tab${createTab === "asset" ? " tab-active" : ""}`}
                onClick={() => setCreateTab("asset")}
              >
                勾選資產租借
              </button>
              <button
                role="tab"
                className={`tab${createTab === "category" ? " tab-active" : ""}`}
                onClick={() => setCreateTab("category")}
              >
                分類數量租借
              </button>
            </div>

            <div className="space-y-4">
              {/* Asset mode */}
              {createTab === "asset" && (
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">選擇資產</span></label>
                  <AssetPicker selected={selectedAssets} onChange={setSelectedAssets} showFilters />
                </div>
              )}

              {/* Category mode */}
              {createTab === "category" && (
                <div className="space-y-2">
                  <div className="grid grid-cols-[1fr_100px_32px] gap-2 text-xs text-base-content/60 px-1">
                    <span>分類</span><span>數量</span><span />
                  </div>
                  {catLines.map((line, i) => (
                    <div key={i} className="grid grid-cols-[1fr_100px_32px] gap-2 items-center">
                      <CategoryLeafSelect
                        categories={categories}
                        value={line.categoryId}
                        onChange={(id) => setCatLines((prev) => prev.map((l, idx) => idx === i ? { ...l, categoryId: id } : l))}
                      />
                      <input
                        type="number"
                        min={1}
                        value={line.quantity}
                        onChange={(e) => setCatLines((prev) => prev.map((l, idx) => idx === i ? { ...l, quantity: Math.max(1, Number(e.target.value)) } : l))}
                        className="input input-bordered input-sm"
                      />
                      <button
                        type="button"
                        onClick={() => setCatLines((prev) => prev.length === 1 ? prev : prev.filter((_, idx) => idx !== i))}
                        className="btn btn-ghost btn-xs btn-circle text-error"
                        disabled={catLines.length === 1}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  ))}
                  <button
                    type="button"
                    onClick={() => setCatLines((prev) => [...prev, { categoryId: "", quantity: 1 }])}
                    className="btn btn-ghost btn-xs gap-1 mt-1"
                  >
                    <Plus size={14} /> 新增一行
                  </button>
                </div>
              )}

              {/* Common fields */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">借用人</span></label>
                  {isViewer ? (
                    <input type="text" value={user?.display_name || user?.username || ""} className="input input-bordered input-sm" disabled />
                  ) : (
                    <select value={borrowerId} onChange={(e) => setBorrowerId(e.target.value)} className="select select-bordered select-sm">
                      <option value="">選擇使用者</option>
                      {users.map((u) => (
                        <option key={u.id} value={u.id}>{u.display_name || u.username}</option>
                      ))}
                    </select>
                  )}
                </div>
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">借出日期</span></label>
                  <input type="date" value={borrowDate} onChange={(e) => setBorrowDate(e.target.value)} className="input input-bordered input-sm" />
                </div>
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">預計歸還日期</span></label>
                  <input type="date" value={expectedReturn} onChange={(e) => setExpectedReturn(e.target.value)} className="input input-bordered input-sm" />
                </div>
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">用途</span></label>
                  <input type="text" value={purpose} onChange={(e) => setPurpose(e.target.value)} className="input input-bordered input-sm" placeholder="借用用途" />
                </div>
                <div className="form-control">
                  <label className="label"><span className="label-text font-medium">備註</span></label>
                  <input type="text" value={notes} onChange={(e) => setNotes(e.target.value)} className="input input-bordered input-sm" placeholder="其他備註" />
                </div>
                {isMultiDay && (
                  <div className="form-control sm:col-span-2">
                    <label className="label">
                      <span className="label-text font-medium">跨日說明</span>
                      <span className="label-text-alt opacity-60">車輛等逐日追蹤分類的跨日租借必填，其他分類可留空</span>
                    </label>
                    <input
                      type="text"
                      value={multiDayReason}
                      onChange={(e) => setMultiDayReason(e.target.value)}
                      className="input input-bordered input-sm"
                      placeholder="例如：出差 3 天"
                    />
                  </div>
                )}
              </div>
              <div className="flex gap-2">
                <button
                  onClick={handleCreateClick}
                  disabled={creating || noticeChecking || !borrowerId || (createTab === "asset" ? selectedAssets.length === 0 : catLines.filter((l) => l.categoryId && l.quantity >= 1).length === 0)}
                  className="btn btn-success btn-sm gap-1"
                >
                  {(creating || noticeChecking) && <span className="loading loading-spinner loading-xs"></span>}
                  提交申請
                </button>
                <button onClick={() => { setShowCreate(false); resetCreateForm(); }} className="btn btn-ghost btn-sm">{t("common.cancel")}</button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Workflow */}
      <div className="flex items-center gap-2 text-xs text-base-content/50 px-1" data-tour="rental-workflow">
        <span className="badge badge-warning badge-xs">待核准</span>
        <span className="text-base-content/30">保管人或管理員核准</span>
        <ArrowRight size={12} />
        <span className="badge badge-info badge-xs">已核准</span>
        <ArrowRight size={12} />
        <span className="badge badge-success badge-xs">借出中</span>
        <ArrowRight size={12} />
        <span className="badge badge-info badge-xs">待核對</span>
        <span className="text-base-content/30">借用人回報，保管人核對</span>
        <ArrowRight size={12} />
        <span className="badge badge-ghost badge-xs">已歸還</span>
      </div>

      {/* Export buttons */}
      {canExport && (
        <div className="flex gap-2 items-center">
          <button onClick={handleExport} disabled={groups.length === 0} className="btn btn-outline btn-sm gap-1">
            <FileDown size={14} /> 匯出記錄
            {selectedNumbers.size > 0 && <span className="badge badge-sm badge-primary">{selectedNumbers.size}</span>}
          </button>
          <button onClick={handleExportAndArchive} disabled={selectedNumbers.size === 0} className="btn btn-secondary btn-sm gap-1">
            <Archive size={14} /> 匯出記錄並存查
            {selectedNumbers.size > 0 && <span className="badge badge-sm">{selectedNumbers.size}</span>}
          </button>
          {selectedNumbers.size > 0 && (
            <span className="text-sm text-base-content/60">
              已選 {selectedNumbers.size} 筆
            </span>
          )}
        </div>
      )}

      {/* Grid */}
      <div className="card bg-base-100 shadow p-2" data-tour="rental-table">
        <DataGrid<RentalGroup>
          rowData={groups}
          columnDefs={columnDefs}
          loading={loading}
          getRowId={(p) => String(p.data.rental_number)}
          overlayNoRowsTemplate={`<span class="opacity-50">尚無租借記錄</span>`}
          masterDetail
          detailCellRendererParams={detailCellRendererParams}
          // AG Grid defaults the detail row slot to 300px (≈7 rows × 36px),
          // which clipped rentals with >7 devices. Letting the slot auto-fit
          // makes >10 devices visible without scrolling inside the detail.
          detailRowAutoHeight
          getRowClass={(p) => p.data?.is_archived ? "opacity-50" : ""}
        />
      </div>
      {/* Return checklist dialog — items resolved dynamically per分類 (Phase 2a) */}
      <dialog className={`modal ${returnGroup ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg">{returnStage === "submit" ? "歸還回報" : "歸還核對"}</h3>
          <p className="text-sm text-base-content/60 mt-1">
            {returnStage === "submit"
              ? "請在裝置還在您手上時填寫以下項目（整批裝置），送出後由保管人核對"
              : "請核對借用人回報的內容（可修正）後完成歸還（整批裝置）"}
          </p>

          {returnStage === "verify" && returnGroup?.daily_tracking_required && verifyDailyReports.length > 0 && (
            <div className="mt-4 border border-base-300 rounded p-2">
              <p className="text-xs font-medium opacity-70 mb-1">每日回報紀錄</p>
              <div className="space-y-1">
                {verifyDailyReports.map((rep) => (
                  <div key={rep.id} className="flex items-center gap-2 text-xs">
                    <span className="font-mono">{rep.report_date}</span>
                    {rep.backfill_reason ? (
                      <span className="badge badge-warning badge-xs" title={rep.backfill_reason}>補登</span>
                    ) : (
                      <span className="badge badge-ghost badge-xs">即時</span>
                    )}
                    <span className="opacity-70">{JSON.stringify(rep.checklist)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {returnItemsLoading ? (
            <div className="flex justify-center py-8"><span className="loading loading-spinner"></span></div>
          ) : returnItems.length === 0 ? (
            <p className="text-sm text-base-content/50 py-4 text-center">此分類沒有設定歸還清點項目</p>
          ) : (
            <div className="space-y-3 mt-4">
              {returnItems.map((item) => {
                const value = returnValues[item.key];
                const setValue = (v: unknown) => setReturnValues((prev) => ({ ...prev, [item.key]: v }));
                if (item.type === "boolean") {
                  return (
                    <label key={item.key} className="flex items-center gap-3 cursor-pointer p-2 rounded hover:bg-base-200">
                      <input
                        type="checkbox"
                        className="checkbox checkbox-sm checkbox-success"
                        checked={value === true}
                        onChange={(e) => setValue(e.target.checked)}
                      />
                      <span className="text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
                    </label>
                  );
                }
                if (item.type === "text") {
                  return (
                    <div key={item.key} className="form-control">
                      <label className="label"><span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span></label>
                      <input type="text" className="input input-bordered input-sm" value={(value as string) || ""} onChange={(e) => setValue(e.target.value)} />
                    </div>
                  );
                }
                if (item.type === "number") {
                  return (
                    <div key={item.key} className="form-control">
                      <label className="label">
                        <span className="label-text text-sm">{item.label}{item.unit ? `（${item.unit}）` : ""}{item.required && <span className="text-error"> *</span>}</span>
                      </label>
                      <input
                        type="number"
                        className="input input-bordered input-sm"
                        value={value === undefined || value === null ? "" : (value as number)}
                        onChange={(e) => setValue(e.target.value === "" ? "" : Number(e.target.value))}
                      />
                    </div>
                  );
                }
                if (item.type === "location") {
                  return <ChecklistLocationField key={item.key} item={item} value={value} onChange={setValue} />;
                }
                return (
                  <ChecklistPhotoField key={item.key} item={item} rentalNumber={returnGroup!.rental_number} value={value} onChange={setValue} />
                );
              })}
            </div>
          )}

          <div className="form-control mt-4">
            <label className="label"><span className="label-text text-sm">備註（選填）</span></label>
            <textarea
              value={returnNotes}
              onChange={(e) => setReturnNotes(e.target.value)}
              placeholder="記錄裝置狀況、損壞情形等"
              className="textarea textarea-bordered textarea-sm"
              rows={2}
            />
          </div>

          {isOverdueVerify && (
            <div className="form-control mt-3">
              <label className="label">
                <span className="label-text text-sm">逾期原因（必填）</span>
                <span className="label-text-alt opacity-60">已超過預計歸還日期 {returnGroup?.expected_return}</span>
              </label>
              <textarea
                value={crossDayReason}
                onChange={(e) => setCrossDayReason(e.target.value)}
                placeholder="例如：交通延誤，隔天才歸還"
                className="textarea textarea-bordered textarea-sm"
                rows={2}
              />
            </div>
          )}

          {!allRequiredFilled && (
            <div role="alert" className="alert alert-warning mt-4 py-2">
              <span className="text-sm">請完成所有必填清點項目</span>
            </div>
          )}

          <div className="modal-action">
            <button className="btn btn-sm" onClick={() => setReturnGroup(null)}>取消</button>
            <button
              className="btn btn-warning btn-sm gap-1"
              disabled={!allRequiredFilled || (isOverdueVerify && !crossDayReason.trim())}
              onClick={confirmReturn}
            >
              <RotateCcw size={14} /> {returnStage === "submit" ? "送出回報" : "確認歸還"}
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setReturnGroup(null)}>close</button>
        </form>
      </dialog>

      {/* Daily report dialog（逐日追蹤分類） */}
      <dialog className={`modal ${dailyReportGroup ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg">每日回報</h3>
          <p className="text-sm text-base-content/60 mt-1">
            單號 {dailyReportGroup?.rental_number}，記錄今天（或補登遺漏的一天）的里程
          </p>

          <div className="form-control mt-4">
            <label className="label"><span className="label-text text-sm">回報日期</span></label>
            <select
              value={dailyReportDate}
              onChange={(e) => setDailyReportDate(e.target.value)}
              className="select select-bordered select-sm"
            >
              {!dailyReportExistingDates.includes(todayStr()) && (
                <option value={todayStr()}>今天（{todayStr()}）</option>
              )}
              <option value="" disabled>— 或補登遺漏的一天 —</option>
              {dailyReportGroup && dailyReportGroup.borrow_date && (() => {
                const start = new Date(dailyReportGroup.borrow_date);
                const end = dailyReportGroup.expected_return ? new Date(dailyReportGroup.expected_return) : new Date();
                const today = todayStr();
                const options: string[] = [];
                for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
                  const s = d.toISOString().slice(0, 10);
                  if (s < today && !dailyReportExistingDates.includes(s)) options.push(s);
                }
                return options.map((s) => <option key={s} value={s}>補登 {s}</option>);
              })()}
            </select>
          </div>

          <div className="form-control mt-3">
            <label className="label"><span className="label-text text-sm">里程數（km）</span></label>
            <input
              type="number"
              value={dailyReportMileage}
              onChange={(e) => setDailyReportMileage(e.target.value)}
              className="input input-bordered input-sm"
              placeholder="例如：12345"
            />
          </div>

          {isDailyReportBackfill && (
            <div className="form-control mt-3">
              <label className="label"><span className="label-text text-sm">補登原因（必填）</span></label>
              <textarea
                value={dailyReportBackfillReason}
                onChange={(e) => setDailyReportBackfillReason(e.target.value)}
                placeholder="例如：忘記填，事後回想"
                className="textarea textarea-bordered textarea-sm"
                rows={2}
              />
            </div>
          )}

          <div className="modal-action">
            <button className="btn btn-sm" onClick={() => setDailyReportGroup(null)}>取消</button>
            <button
              className="btn btn-primary btn-sm gap-1"
              disabled={dailyReportSubmitting || !dailyReportDate || (isDailyReportBackfill && !dailyReportBackfillReason.trim())}
              onClick={confirmDailyReport}
            >
              {dailyReportSubmitting && <span className="loading loading-spinner loading-xs"></span>}
              <Gauge size={14} /> 送出回報
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setDailyReportGroup(null)}>close</button>
        </form>
      </dialog>

      {/* 分類注意事項同意 dialog（Phase 3）——攔截提交，須全部勾選同意才能繼續 */}
      <dialog className={`modal ${noticePending.length > 0 ? "modal-open" : ""}`}>
        <div className="modal-box max-w-xl">
          <h3 className="font-bold text-lg">請詳閱以下注意事項</h3>
          <p className="text-sm text-base-content/60 mt-1">此次租借涉及的分類設有使用須知，需全部閱讀並同意才能送出申請</p>

          <div className="space-y-3 mt-4 max-h-[50vh] overflow-y-auto">
            {noticePending.map((n) => (
              <div key={n.category_id} className="p-3 rounded border border-base-300">
                <p className="text-sm whitespace-pre-wrap">{n.content}</p>
                <label className="flex items-center gap-2 text-sm mt-2 cursor-pointer">
                  <input
                    type="checkbox"
                    className="checkbox checkbox-sm"
                    checked={!!noticeAgreed[n.category_id]}
                    onChange={(e) => setNoticeAgreed((prev) => ({ ...prev, [n.category_id]: e.target.checked }))}
                  />
                  我已閱讀並同意
                </label>
              </div>
            ))}
          </div>

          <div className="modal-action">
            <button className="btn btn-sm" onClick={() => setNoticePending([])}>{t("common.cancel")}</button>
            <button
              className="btn btn-primary btn-sm gap-1"
              disabled={noticeSubmitting || !allNoticesAgreed}
              onClick={confirmNoticesAndCreate}
            >
              {noticeSubmitting && <span className="loading loading-spinner loading-xs"></span>}
              同意並送出申請
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setNoticePending([])}>close</button>
        </form>
      </dialog>
    </div>
  );
}
