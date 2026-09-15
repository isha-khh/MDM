import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useDialog } from "../components/DialogProvider";
import { Link } from "react-router";
import { Search, Plus, Download, Filter, X, Edit3, Trash2, FileDown, Upload, Layers } from "lucide-react";
import type { ColDef, GridApi, GridReadyEvent, ICellRendererParams, ModelUpdatedEvent } from "ag-grid-enterprise";
import apiClient from "../lib/apiClient";
import { AssetForm } from "../components/AssetForm";
import { DataGrid } from "../components/DataGrid";
import { BatchEditAssetsDialog } from "../components/BatchEditAssetsDialog";

interface AssetRow {
  id: string;
  device_udid: string | null;
  asset_number: string;
  name: string;
  spec: string;
  quantity: number;
  unit: string;
  acquired_date: string | null;
  unit_price: number;
  purpose: string;
  assigned_date: string | null;
  custodian_id: string | null;
  custodian_name: string;
  current_holder_id: string | null;
  current_holder_name: string;
  location: string;
  asset_category: string;
  category_id: string | null;
  category_name: string;
  asset_status: string;
  device_name: string;
  device_serial: string;
  notes: string;
  is_rentable: boolean;
}

interface CategoryOption { id: string; name: string; level: number; }
interface UserOption { id: string; username: string; display_name: string; }

const ASSET_STATUS_CONFIG: Record<string, { label: string; badge: string }> = {
  available:    { label: "可用",   badge: "badge-success" },
  rented:       { label: "借出",   badge: "badge-warning" },
  faulty:       { label: "故障",   badge: "badge-error" },
  repairing:    { label: "維修中", badge: "badge-info" },
  lost:         { label: "遺失",   badge: "badge-error" },
  retired:      { label: "報廢",   badge: "badge-ghost" },
  transferred:  { label: "移撥",   badge: "badge-ghost" },
};

export function AssetList() {
  const { t } = useTranslation();
  const dialog = useDialog();
  const [assets, setAssets] = useState<AssetRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [categories, setCategories] = useState<CategoryOption[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [filterCategory, setFilterCategory] = useState("");
  const [filterCustodian, setFilterCustodian] = useState("");
  const [filterStatus, setFilterStatus] = useState("");
  const [filterLinked, setFilterLinked] = useState(""); // "mdm" | "standalone" | ""
  const [editingAssetId, setEditingAssetId] = useState<string | null>(null); // asset id or "new"
  const [importing, setImporting] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [showBatchEdit, setShowBatchEdit] = useState(false);
  const gridApiRef = useRef<GridApi<AssetRow> | null>(null);
  // IDs actually rendered by AG Grid right now — reflects BOTH our own
  // top-toolbar filters (search/category/custodian/status/linked) AND AG
  // Grid's own built-in per-column filters (enabled by DataGrid's default
  // colDef `filter: true`). `filtered` alone only tracks the former, which
  // let "select all" grab rows that were hidden by a column filter.
  const [visibleIds, setVisibleIds] = useState<string[]>([]);

  const syncVisibleIds = (api: GridApi<AssetRow>) => {
    const ids: string[] = [];
    api.forEachNodeAfterFilterAndSort((node) => {
      if (node.data) ids.push(node.data.id);
    });
    setVisibleIds(ids);
  };

  const onGridReady = (e: GridReadyEvent<AssetRow>) => {
    gridApiRef.current = e.api;
    syncVisibleIds(e.api);
  };

  const onModelUpdated = (e: ModelUpdatedEvent<AssetRow>) => {
    syncVisibleIds(e.api);
  };

  const loadAssets = async () => {
    setLoading(true);
    try {
      const { data } = await apiClient.get("/api/assets");
      setAssets(data.assets || []);
    } catch (err) { console.error("Load assets:", err); }
    finally { setLoading(false); }
  };

  useEffect(() => {
    loadAssets();
    apiClient.get("/api/categories").then(({ data }) => setCategories(data.categories || [])).catch(() => {});
    apiClient.get("/api/users-list").then(({ data }) => setUsers(data.users || [])).catch(() => {});
  }, []);

  const handleImport = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (!/\.xlsx$/i.test(file.name)) {
      await dialog.error(t("assets.importBadFormat"));
      if (fileRef.current) fileRef.current.value = "";
      return;
    }
    setImporting(true);
    try {
      const form = new FormData();
      form.append("file", file);
      const { data } = await apiClient.post("/api/assets-import", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      const lines = [
        t("assets.importCreated", { count: data.created ?? 0 }),
        t("assets.importFailed", { count: data.failed ?? 0 }),
      ];
      if (Array.isArray(data.errors) && data.errors.length > 0) {
        lines.push("");
        lines.push(...data.errors.slice(0, 20));
        if (data.errors.length > 20) lines.push(`… (+${data.errors.length - 20})`);
      }
      await dialog.alert(lines.join("\n"));
      loadAssets();
    } catch (err: any) {
      await dialog.error(t("assets.importError") + ": " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setImporting(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const deleteAsset = async (id: string) => {
    if (!(await dialog.confirm("確定要刪除此財產資訊？"))) return;
    try {
      await apiClient.delete(`/api/assets/${id}`);
      loadAssets();
    } catch { await dialog.error("刪除失敗"); }
  };

  const hasFilters = filterCategory || filterCustodian || filterStatus || filterLinked;
  const clearFilters = () => { setFilterCategory(""); setFilterCustodian(""); setFilterStatus(""); setFilterLinked(""); };

  // Client-side filtering
  const filtered = assets.filter((a) => {
    if (search) {
      const q = search.toLowerCase();
      if (
        !a.asset_number.toLowerCase().includes(q) &&
        !a.name.toLowerCase().includes(q) &&
        !a.custodian_name.toLowerCase().includes(q) &&
        !(a.device_name || "").toLowerCase().includes(q) &&
        !(a.device_serial || "").toLowerCase().includes(q) &&
        !a.spec.toLowerCase().includes(q)
      ) return false;
    }
    if (filterCategory && a.category_id !== filterCategory) return false;
    if (filterCustodian && a.custodian_id !== filterCustodian) return false;
    if (filterStatus && a.asset_status !== filterStatus) return false;
    if (filterLinked === "mdm" && !a.device_udid) return false;
    if (filterLinked === "standalone" && a.device_udid) return false;
    return true;
  });

  const categoryOptions = categories.map((c) => ({
    ...c,
    label: "\u00A0\u00A0".repeat(c.level) + c.name,
  }));

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const toggleSelectAllVisible = () => {
    const allSelected = visibleIds.length > 0 && visibleIds.every((id) => selectedIds.has(id));
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (allSelected) {
        visibleIds.forEach((id) => next.delete(id));
      } else {
        visibleIds.forEach((id) => next.add(id));
      }
      return next;
    });
  };

  const columnDefs = useMemo<ColDef<AssetRow>[]>(() => [
    {
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
          checked={visibleIds.length > 0 && visibleIds.every((id) => selectedIds.has(id))}
          onChange={toggleSelectAllVisible}
        />
      ),
      cellRenderer: (p: ICellRendererParams<AssetRow>) => (
        <input
          type="checkbox"
          className="checkbox checkbox-xs"
          checked={selectedIds.has(p.data!.id)}
          onChange={() => toggleSelect(p.data!.id)}
        />
      ),
    },
    {
      headerName: t("assets.assetNumber"),
      field: "asset_number",
      width: 140,
      cellClass: "font-mono text-xs",
      valueFormatter: (p) => p.value || "-",
    },
    {
      headerName: t("assets.name"),
      field: "name",
      minWidth: 100,
      cellClass: "font-medium",
      valueFormatter: (p) => p.value || "-",

    },
    {
      headerName: t("assets.spec"),
      field: "spec",
      minWidth: 140,
      cellClass: "text-sm opacity-70",
      valueFormatter: (p) => p.value || "-",
      hide:true
    },
    {
      headerName: "分類",
      field: "category_name",
      width: 140,
      cellRenderer: (p: ICellRendererParams<AssetRow>) =>
        p.value
          ? <span className="badge badge-ghost badge-sm">{p.value}</span>
          : <span className="opacity-30">-</span>,
    },
    {
      headerName: t("assets.custodian"),
      field: "custodian_name",
      width: 120,
      valueFormatter: (p) => p.value || "-",
    },
    {
      headerName: t("assets.currentHolder"),
      field: "current_holder_name",
      width: 140,
      cellRenderer: (p: ICellRendererParams<AssetRow>) =>
        p.value
          ? <span className="badge badge-warning badge-sm">{p.value}</span>
          : <span className="opacity-30">-</span>,
    },
    {
      headerName: "狀態",
      field: "asset_status",
      width: 100,
      cellRenderer: (p: ICellRendererParams<AssetRow>) => {
        const st = ASSET_STATUS_CONFIG[p.value as string] || ASSET_STATUS_CONFIG.available;
        return <span className={`badge badge-sm ${st.badge}`}>{st.label}</span>;
      },
    },
    {
      headerName: "可租借",
      field: "is_rentable",
      width: 90,
      cellRenderer: (p: ICellRendererParams<AssetRow>) =>
        p.data?.is_rentable
          ? <span className="badge badge-success badge-sm">是</span>
          : <span className="badge badge-ghost badge-sm">否</span>,
    },
    {
      headerName: "關聯裝置",
      field: "device_udid",
      width: 160,
      cellRenderer: (p: ICellRendererParams<AssetRow>) => {
        const a = p.data!;
        return a.device_udid ? (
          <Link to={`/mdm/devices/${a.device_udid}`} className="link link-primary text-xs">
            {a.device_name || a.device_serial || a.device_udid.substring(0, 8)}
          </Link>
        ) : (
          <span className="badge badge-outline badge-sm">獨立資產</span>
        );
      },
    },
    {
      headerName: t("assets.location"),
      field: "location",
      minWidth: 120,
      cellClass: "text-sm opacity-70",
      valueFormatter: (p) => p.value || "-",
    },
    {
      headerName: t("common.actions"),
      colId: "actions",
      width: 100,
      sortable: false,
      filter: false,
      pinned: "right",
      cellRenderer: (p: ICellRendererParams<AssetRow>) => (
        <div className="flex gap-1 h-full items-center">
          <button onClick={() => setEditingAssetId(p.data!.id)} className="btn btn-ghost btn-xs"><Edit3 size={14} /></button>
          <button onClick={() => deleteAsset(p.data!.id)} className="btn btn-ghost btn-xs text-error"><Trash2 size={14} /></button>
        </div>
      ),
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ], [t, visibleIds, selectedIds]);

  // Editing inline
  if (editingAssetId) {
    const editingAsset = editingAssetId === "new" ? null : assets.find((a) => a.id === editingAssetId);
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold">{t("nav.assets")}</h1>
          <button onClick={() => setEditingAssetId(null)} className="btn btn-ghost btn-sm gap-1">
            <X size={14} /> {t("common.cancel")}
          </button>
        </div>
        <div className="card bg-base-100 shadow p-6">
          <AssetForm
            assetId={editingAssetId === "new" ? undefined : editingAssetId}
            deviceUdid={editingAsset?.device_udid || undefined}
            onSaved={() => { setEditingAssetId(null); loadAssets(); }}
            onCancel={() => setEditingAssetId(null)}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">{t("nav.assets")}</h1>
          <p className="text-sm text-base-content/60">{t("common.showing", { count: filtered.length })}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <label className="input input-bordered input-sm flex items-center gap-2">
            <Search size={14} className="opacity-50" />
            <input
              type="text"
              placeholder={t("common.search")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="grow w-32"
            />
          </label>
          {selectedIds.size > 0 && (
            <button onClick={() => setShowBatchEdit(true)} className="btn btn-secondary btn-sm gap-1">
              <Layers size={14} /> {t("assets.batchEdit.button")}
              <span className="badge badge-sm">{selectedIds.size}</span>
            </button>
          )}
          <button onClick={() => setEditingAssetId("new")} className="btn btn-primary btn-sm gap-1">
            <Plus size={14} /> {t("assets.add")}
          </button>
          <button onClick={() => window.open("/api/assets-template", "_blank")} className="btn btn-ghost btn-sm gap-1">
            <FileDown size={14} /> {t("assets.downloadTemplate")}
          </button>
          <button
            onClick={() => fileRef.current?.click()}
            disabled={importing}
            className="btn btn-outline btn-sm gap-1"
          >
            {importing
              ? <span className="loading loading-spinner loading-xs"></span>
              : <Upload size={14} />}
            {importing ? t("assets.importing") : t("assets.import")}
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".xlsx"
            className="hidden"
            onChange={handleImport}
          />
          <button onClick={() => window.open("/api/assets-export", "_blank")} className="btn btn-outline btn-sm gap-1">
            <Download size={14} /> Excel
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-2 items-center">
        <Filter size={14} className="opacity-50" />
        <select value={filterCategory} onChange={(e) => setFilterCategory(e.target.value)} className="select select-bordered select-sm">
          <option value="">全部分類</option>
          {categoryOptions.map((c) => <option key={c.id} value={c.id}>{c.label}</option>)}
        </select>
        <select value={filterCustodian} onChange={(e) => setFilterCustodian(e.target.value)} className="select select-bordered select-sm">
          <option value="">全部保管人</option>
          {users.map((u) => <option key={u.id} value={u.id}>{u.display_name || u.username}</option>)}
        </select>
        <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)} className="select select-bordered select-sm">
          <option value="">全部狀態</option>
          {Object.entries(ASSET_STATUS_CONFIG).map(([val, cfg]) => (
            <option key={val} value={val}>{cfg.label}</option>
          ))}
        </select>
        <select value={filterLinked} onChange={(e) => setFilterLinked(e.target.value)} className="select select-bordered select-sm">
          <option value="">全部來源</option>
          <option value="mdm">MDM 裝置</option>
          <option value="standalone">獨立資產</option>
        </select>
        {hasFilters && (
          <button onClick={clearFilters} className="btn btn-ghost btn-sm gap-1">
            <X size={14} /> 清除篩選
          </button>
        )}
      </div>

      {/* Grid */}
      <div className="card bg-base-100 shadow p-2">
        <DataGrid<AssetRow>
          rowData={filtered}
          columnDefs={columnDefs}
          loading={loading}
          getRowId={(p) => p.data.id}
          onGridReady={onGridReady}
          onModelUpdated={onModelUpdated}
          overlayNoRowsTemplate={`<span class="opacity-50">${t("assets.noAsset")}</span>`}
        />
      </div>

      <BatchEditAssetsDialog
        open={showBatchEdit}
        assetIds={Array.from(selectedIds)}
        onClose={() => setShowBatchEdit(false)}
        onDone={() => { setSelectedIds(new Set()); loadAssets(); }}
      />
    </div>
  );
}
