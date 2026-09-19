import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import apiClient from "../lib/apiClient";
import { useDialog } from "../components/DialogProvider";
import { Plus, Trash2, Edit3, Save, X, ChevronRight, FolderTree, ClipboardList, ArrowUp, ArrowDown, AlertTriangle } from "lucide-react";

interface Category {
  id: string;
  parent_id: string | null;
  name: string;
  level: number;
  sort_order: number;
}

type ChecklistItemType = "boolean" | "text" | "number" | "location" | "photo";

interface ChecklistItem {
  key: string;
  label: string;
  type: ChecklistItemType;
  required: boolean;
  unit?: string;
  maxCount?: number;
}

const CHECKLIST_TYPE_LABELS: Record<ChecklistItemType, string> = {
  boolean: "勾選",
  text: "文字",
  number: "數值",
  location: "定位",
  photo: "照片",
};

// Slugify a label into a stable, ASCII item key; falls back to a counter
// suffix on collision so two similarly-named items don't clash.
function slugifyKey(label: string, existing: Set<string>): string {
  const base = label.trim().toLowerCase().replace(/[^a-z0-9一-鿿]+/g, "_").replace(/^_+|_+$/g, "") || "item";
  let key = base;
  let i = 2;
  while (existing.has(key)) { key = `${base}_${i++}`; }
  return key;
}

export function Categories() {
  const { t } = useTranslation();
  const dialog = useDialog();
  const [categories, setCategories] = useState<Category[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [addParentId, setAddParentId] = useState<string | null>(null);
  const [addName, setAddName] = useState("");
  const [saving, setSaving] = useState(false);

  // 逐日追蹤規則（僅葉節點分類適用；跨日租借需說明、期間每天各填一份 checklist，
  // 用於車輛里程等記錄）。以 category_id 為 key，值 = daily_tracking_required。
  const [rentalRules, setRentalRules] = useState<Record<string, boolean>>({});
  const [savingRuleId, setSavingRuleId] = useState<string | null>(null);

  const loadRentalRules = async (cats: Category[]) => {
    const leafIds = cats.filter((c) => !cats.some((child) => child.parent_id === c.id)).map((c) => c.id);
    const entries = await Promise.all(leafIds.map(async (id): Promise<[string, boolean]> => {
      try {
        const { data } = await apiClient.get(`/api/categories/${id}/rental-rule`);
        return [id, !!data.daily_tracking_required];
      } catch { return [id, false]; }
    }));
    setRentalRules(Object.fromEntries(entries));
  };

  const load = async () => {
    setLoading(true);
    try {
      const { data } = await apiClient.get("/api/categories");
      const cats: Category[] = data.categories || [];
      setCategories(cats);
      loadRentalRules(cats);
    } catch (err) { console.error(err); }
    finally { setLoading(false); }
  };

  useEffect(() => { load(); }, []);

  const toggleDailyTracking = async (id: string, value: boolean) => {
    setSavingRuleId(id);
    try {
      await apiClient.put(`/api/categories/${id}/rental-rule`, { daily_tracking_required: value });
      setRentalRules((prev) => ({ ...prev, [id]: value }));
    } catch { await dialog.error("設定失敗"); }
    finally { setSavingRuleId(null); }
  };

  // 歸還檢查清單範本編輯（Phase 2a）
  const [checklistCategoryId, setChecklistCategoryId] = useState<string | null>(null);
  const [checklistItems, setChecklistItems] = useState<ChecklistItem[]>([]);
  const [checklistIsExplicit, setChecklistIsExplicit] = useState(false);
  const [checklistLoading, setChecklistLoading] = useState(false);
  const [checklistSaving, setChecklistSaving] = useState(false);

  const openChecklistEditor = async (cat: Category) => {
    setChecklistCategoryId(cat.id);
    setChecklistLoading(true);
    try {
      const { data } = await apiClient.get(`/api/categories/${cat.id}/checklist-template`);
      setChecklistItems(data.items || []);
      setChecklistIsExplicit(!!data.is_explicit);
    } catch {
      setChecklistItems([]);
      setChecklistIsExplicit(false);
    } finally { setChecklistLoading(false); }
  };

  const addChecklistItem = () => {
    setChecklistItems((prev) => [
      ...prev,
      { key: slugifyKey("項目", new Set(prev.map((i) => i.key))), label: "", type: "boolean", required: true },
    ]);
  };

  const updateChecklistItem = (idx: number, patch: Partial<ChecklistItem>) => {
    setChecklistItems((prev) => prev.map((it, i) => {
      if (i !== idx) return it;
      const next = { ...it, ...patch };
      // Re-derive the key from the label when the label changes and the key
      // still looks auto-generated (i.e. the admin hasn't customized it) —
      // keeps keys readable without requiring manual slug editing.
      if (patch.label !== undefined) {
        const others = new Set(prev.filter((_, j) => j !== idx).map((o) => o.key));
        next.key = slugifyKey(patch.label, others);
      }
      return next;
    }));
  };

  const removeChecklistItem = (idx: number) => {
    setChecklistItems((prev) => prev.filter((_, i) => i !== idx));
  };

  const moveChecklistItem = (idx: number, dir: -1 | 1) => {
    setChecklistItems((prev) => {
      const next = [...prev];
      const target = idx + dir;
      if (target < 0 || target >= next.length) return prev;
      [next[idx], next[target]] = [next[target], next[idx]];
      return next;
    });
  };

  const saveChecklistTemplate = async () => {
    if (!checklistCategoryId) return;
    const cleaned = checklistItems.filter((it) => it.label.trim() !== "");
    setChecklistSaving(true);
    try {
      await apiClient.put(`/api/categories/${checklistCategoryId}/checklist-template`, { items: cleaned });
      setChecklistCategoryId(null);
    } catch { await dialog.error("儲存失敗"); }
    finally { setChecklistSaving(false); }
  };

  // 租借注意事項編輯（Phase 3）——提交租借申請前需借用人同意；沒有全域預設，
  // 分類鏈上都沒設定就代表不用跳出來。
  const [noticeCategoryId, setNoticeCategoryId] = useState<string | null>(null);
  const [noticeContent, setNoticeContent] = useState("");
  const [noticeIsExplicit, setNoticeIsExplicit] = useState(false);
  const [noticeLoading, setNoticeLoading] = useState(false);
  const [noticeSaving, setNoticeSaving] = useState(false);

  const openNoticeEditor = async (cat: Category) => {
    setNoticeCategoryId(cat.id);
    setNoticeLoading(true);
    try {
      const { data } = await apiClient.get(`/api/categories/${cat.id}/notice`);
      setNoticeContent(data.content || "");
      setNoticeIsExplicit(!!data.is_explicit);
    } catch {
      setNoticeContent("");
      setNoticeIsExplicit(false);
    } finally { setNoticeLoading(false); }
  };

  const saveNotice = async () => {
    if (!noticeCategoryId) return;
    setNoticeSaving(true);
    try {
      await apiClient.put(`/api/categories/${noticeCategoryId}/notice`, { content: noticeContent.trim() });
      setNoticeCategoryId(null);
    } catch { await dialog.error("儲存失敗"); }
    finally { setNoticeSaving(false); }
  };

  // Build tree structure for rendering
  const tree = useMemo(() => {
    const roots = categories.filter((c) => !c.parent_id);
    const getChildren = (parentId: string): Category[] =>
      categories.filter((c) => c.parent_id === parentId).sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
    return { roots, getChildren };
  }, [categories]);

  const handleAdd = async (parentId: string | null) => {
    if (!addName.trim()) return;
    setSaving(true);
    try {
      await apiClient.post("/api/categories", { parent_id: parentId, name: addName.trim() });
      setAddParentId(null);
      setAddName("");
      load();
    } catch (err) { await dialog.error("新增失敗"); }
    finally { setSaving(false); }
  };

  const handleUpdate = async () => {
    if (!editingId || !editName.trim()) return;
    setSaving(true);
    try {
      await apiClient.put(`/api/categories/${editingId}`, { name: editName.trim() });
      setEditingId(null);
      load();
    } catch (err) { await dialog.error("更新失敗"); }
    finally { setSaving(false); }
  };

  const handleDelete = async (id: string, name: string) => {
    const children = categories.filter((c) => c.parent_id === id);
    const msg = children.length > 0
      ? `刪除「${name}」及其 ${children.length} 個子分類？`
      : `刪除「${name}」？`;
    if (!(await dialog.confirm(msg))) return;
    try {
      await apiClient.delete(`/api/categories/${id}`);
      load();
    } catch { await dialog.error("刪除失敗"); }
  };

  const renderCategory = (cat: Category, depth: number = 0) => {
    const children = tree.getChildren(cat.id);
    const isEditing = editingId === cat.id;
    const isAdding = addParentId === cat.id;

    return (
      <div key={cat.id}>
        <div className={`flex items-center gap-2 py-1.5 px-2 rounded hover:bg-base-200 group`} style={{ paddingLeft: `${depth * 24 + 8}px` }}>
          {children.length > 0 ? (
            <ChevronRight size={14} className="opacity-40" />
          ) : (
            <span className="w-3.5" />
          )}

          {isEditing ? (
            <div className="flex items-center gap-1 flex-1">
              <input
                type="text"
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleUpdate()}
                className="input input-bordered input-xs flex-1"
                autoFocus
              />
              <button onClick={handleUpdate} disabled={saving} className="btn btn-success btn-xs"><Save size={12} /></button>
              <button onClick={() => setEditingId(null)} className="btn btn-ghost btn-xs"><X size={12} /></button>
            </div>
          ) : (
            <>
              <span className={`flex-1 text-sm ${depth === 0 ? "font-bold" : depth === 1 ? "font-medium" : ""}`}>
                {cat.name}
              </span>
              {children.length === 0 && (
                <>
                  <button
                    onClick={() => openChecklistEditor(cat)}
                    className="btn btn-ghost btn-xs gap-1"
                    title="編輯此分類歸還時的檢查清單（未設定時套用上層分類或全域預設）"
                  >
                    <ClipboardList size={12} /> 歸還清單
                  </button>
                  <button
                    onClick={() => openNoticeEditor(cat)}
                    className="btn btn-ghost btn-xs gap-1"
                    title="編輯此分類的租借注意事項（提交申請前需借用人閱讀同意；未設定時不套用任何上層的話則不會跳出）"
                  >
                    <AlertTriangle size={12} /> 注意事項
                  </button>
                  <label
                    className="flex items-center gap-1 text-xs opacity-70 cursor-pointer whitespace-nowrap"
                    title="開啟後：跨日租借需填寫說明，且租借期間每天各自要填一份檢查清單（例如車輛里程），用於逐日記錄"
                  >
                    <input
                      type="checkbox"
                      className="toggle toggle-xs"
                      checked={!!rentalRules[cat.id]}
                      disabled={savingRuleId === cat.id}
                      onChange={(e) => toggleDailyTracking(cat.id, e.target.checked)}
                    />
                    逐日追蹤
                  </label>
                </>
              )}
              <div className="opacity-0 group-hover:opacity-100 flex gap-0.5 transition-opacity">
                <button onClick={() => { setAddParentId(cat.id); setAddName(""); }} className="btn btn-ghost btn-xs" title="新增子分類">
                  <Plus size={12} />
                </button>
                <button onClick={() => { setEditingId(cat.id); setEditName(cat.name); }} className="btn btn-ghost btn-xs">
                  <Edit3 size={12} />
                </button>
                <button onClick={() => handleDelete(cat.id, cat.name)} className="btn btn-ghost btn-xs text-error">
                  <Trash2 size={12} />
                </button>
              </div>
            </>
          )}
        </div>

        {/* Add child form */}
        {isAdding && (
          <div className="flex items-center gap-1 py-1" style={{ paddingLeft: `${(depth + 1) * 24 + 8}px` }}>
            <Plus size={12} className="opacity-40" />
            <input
              type="text"
              value={addName}
              onChange={(e) => setAddName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleAdd(cat.id)}
              placeholder="新分類名稱"
              className="input input-bordered input-xs flex-1"
              autoFocus
            />
            <button onClick={() => handleAdd(cat.id)} disabled={saving} className="btn btn-success btn-xs"><Save size={12} /></button>
            <button onClick={() => setAddParentId(null)} className="btn btn-ghost btn-xs"><X size={12} /></button>
          </div>
        )}

        {/* Children */}
        {children.map((child) => renderCategory(child, depth + 1))}
      </div>
    );
  };

  if (loading) {
    return <div className="flex justify-center py-12"><span className="loading loading-spinner loading-lg"></span></div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">裝置分類管理</h1>
          <p className="text-sm text-base-content/60">管理品牌、類別、型號的樹狀分類</p>
        </div>
        <button onClick={() => { setAddParentId("__root__"); setAddName(""); }} className="btn btn-primary btn-sm gap-1">
          <Plus size={14} /> 新增頂層分類
        </button>
      </div>

      {/* Add root form */}
      {addParentId === "__root__" && (
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={addName}
            onChange={(e) => setAddName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleAdd(null)}
            placeholder="頂層分類名稱（如：Apple）"
            className="input input-bordered input-sm flex-1"
            autoFocus
          />
          <button onClick={() => handleAdd(null)} disabled={saving} className="btn btn-success btn-sm gap-1">
            {saving && <span className="loading loading-spinner loading-xs"></span>}
            新增
          </button>
          <button onClick={() => setAddParentId(null)} className="btn btn-ghost btn-sm">{t("common.cancel")}</button>
        </div>
      )}

      <div className="card bg-base-100 shadow">
        <div className="card-body p-4">
          {tree.roots.length === 0 ? (
            <div className="text-center py-8 text-base-content/50 flex flex-col items-center gap-2">
              <FolderTree size={32} className="opacity-30" />
              <p>尚無分類，點擊「新增頂層分類」開始</p>
            </div>
          ) : (
            <div className="divide-y divide-base-200">
              {tree.roots.map((root) => renderCategory(root))}
            </div>
          )}
        </div>
      </div>

      {/* Checklist template editor (Phase 2a) */}
      <dialog className={`modal ${checklistCategoryId ? "modal-open" : ""}`}>
        <div className="modal-box max-w-2xl">
          <h3 className="font-bold text-lg">
            歸還檢查清單 — {categories.find((c) => c.id === checklistCategoryId)?.name}
          </h3>
          <p className="text-sm text-base-content/60 mt-1">
            {checklistIsExplicit
              ? "此分類已有自己的設定"
              : "此分類目前套用上層分類或全域預設範本；儲存後會改用下面這份自己的設定"}
          </p>

          {checklistLoading ? (
            <div className="flex justify-center py-8"><span className="loading loading-spinner"></span></div>
          ) : (
            <div className="space-y-2 mt-4 max-h-[50vh] overflow-y-auto">
              {checklistItems.length === 0 && (
                <p className="text-sm text-base-content/50 py-4 text-center">尚無項目，點擊下方「新增項目」開始</p>
              )}
              {checklistItems.map((item, idx) => (
                <div key={idx} className="flex items-start gap-2 p-2 rounded border border-base-300">
                  <div className="flex flex-col gap-0.5">
                    <button className="btn btn-ghost btn-xs btn-square" disabled={idx === 0} onClick={() => moveChecklistItem(idx, -1)}><ArrowUp size={10} /></button>
                    <button className="btn btn-ghost btn-xs btn-square" disabled={idx === checklistItems.length - 1} onClick={() => moveChecklistItem(idx, 1)}><ArrowDown size={10} /></button>
                  </div>
                  <div className="flex-1 grid grid-cols-1 sm:grid-cols-[1fr_100px_auto_auto] gap-2 items-center">
                    <input
                      type="text"
                      value={item.label}
                      onChange={(e) => updateChecklistItem(idx, { label: e.target.value })}
                      placeholder="項目名稱，例如：里程數"
                      className="input input-bordered input-sm"
                    />
                    <select
                      value={item.type}
                      onChange={(e) => updateChecklistItem(idx, { type: e.target.value as ChecklistItemType })}
                      className="select select-bordered select-sm"
                    >
                      {(Object.keys(CHECKLIST_TYPE_LABELS) as ChecklistItemType[]).map((t) => (
                        <option key={t} value={t}>{CHECKLIST_TYPE_LABELS[t]}</option>
                      ))}
                    </select>
                    {item.type === "number" && (
                      <input
                        type="text"
                        value={item.unit || ""}
                        onChange={(e) => updateChecklistItem(idx, { unit: e.target.value })}
                        placeholder="單位，如 km"
                        className="input input-bordered input-sm w-24"
                      />
                    )}
                    {item.type === "photo" && (
                      <input
                        type="number"
                        min={1}
                        value={item.maxCount || 1}
                        onChange={(e) => updateChecklistItem(idx, { maxCount: Math.max(1, Number(e.target.value)) })}
                        placeholder="張數上限"
                        className="input input-bordered input-sm w-24"
                        title="照片張數上限"
                      />
                    )}
                    <label className="flex items-center gap-1 text-xs whitespace-nowrap">
                      <input
                        type="checkbox"
                        className="checkbox checkbox-xs"
                        checked={item.required}
                        onChange={(e) => updateChecklistItem(idx, { required: e.target.checked })}
                      />
                      必填
                    </label>
                  </div>
                  <button onClick={() => removeChecklistItem(idx)} className="btn btn-ghost btn-xs btn-square text-error"><Trash2 size={12} /></button>
                </div>
              ))}
              <button onClick={addChecklistItem} className="btn btn-ghost btn-sm gap-1 mt-1"><Plus size={14} /> 新增項目</button>
            </div>
          )}

          <div className="modal-action">
            <button className="btn btn-sm" onClick={() => setChecklistCategoryId(null)}>{t("common.cancel")}</button>
            <button className="btn btn-primary btn-sm gap-1" disabled={checklistSaving || checklistLoading} onClick={saveChecklistTemplate}>
              {checklistSaving && <span className="loading loading-spinner loading-xs"></span>}
              <Save size={14} /> 儲存
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setChecklistCategoryId(null)}>close</button>
        </form>
      </dialog>

      {/* Notice editor (Phase 3) */}
      <dialog className={`modal ${noticeCategoryId ? "modal-open" : ""}`}>
        <div className="modal-box max-w-xl">
          <h3 className="font-bold text-lg">
            租借注意事項 — {categories.find((c) => c.id === noticeCategoryId)?.name}
          </h3>
          <p className="text-sm text-base-content/60 mt-1">
            {noticeIsExplicit
              ? "此分類已有自己的設定"
              : "此分類目前沒有設定（也沒有從上層分類繼承到），送出租借申請時不會跳出提醒；儲存非空白內容後才會開始套用"}
          </p>

          {noticeLoading ? (
            <div className="flex justify-center py-8"><span className="loading loading-spinner"></span></div>
          ) : (
            <textarea
              value={noticeContent}
              onChange={(e) => setNoticeContent(e.target.value)}
              placeholder="例如：使用車輛前請確認油量、行照隨車攜帶，發生事故請立即通知保管人…（留空 = 清除此分類自己的設定，改回繼承上層或不套用）"
              className="textarea textarea-bordered w-full mt-4 min-h-40"
            />
          )}

          <div className="modal-action">
            <button className="btn btn-sm" onClick={() => setNoticeCategoryId(null)}>{t("common.cancel")}</button>
            <button className="btn btn-primary btn-sm gap-1" disabled={noticeSaving || noticeLoading} onClick={saveNotice}>
              {noticeSaving && <span className="loading loading-spinner loading-xs"></span>}
              <Save size={14} /> 儲存
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setNoticeCategoryId(null)}>close</button>
        </form>
      </dialog>
    </div>
  );
}
