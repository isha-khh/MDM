import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import apiClient from "../lib/apiClient";
import { useDialog } from "../components/DialogProvider";
import { Plus, Trash2, Edit3, Save, X, ChevronRight, FolderTree } from "lucide-react";

interface Category {
  id: string;
  parent_id: string | null;
  name: string;
  level: number;
  sort_order: number;
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
    </div>
  );
}
