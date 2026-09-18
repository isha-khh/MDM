import { useState, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Search, X, Tablet, Package, Check, Hash, MapPin } from "lucide-react";
import apiClient from "../lib/apiClient";

interface CategoryOption { id: string; name: string; level: number; }

interface AssetItem {
  asset_id: string;
  asset_number: string;
  name: string;
  spec: string;
  device_udid: string | null;
  serial_number: string;
  model: string;
  os_version: string;
  asset_status: string;
  category_id: string | null;
  category_name: string;
  // Phase 2c of 租借 2.1: set when a past rental's return checklist
  // included a "location" type answer for this asset. Separate from any
  // manually-maintained storage location — only shown when present, so
  // most (non-vehicle) assets show nothing extra here.
  last_return_location?: { lat?: number; lng?: number; address?: string } | null;
  last_return_at?: string | null;
}

interface AssetPickerProps {
  selected: string[];                   // asset_ids
  onChange: (assetIds: string[]) => void;
  showFilters?: boolean;
  /** Source endpoint for the pickable list. Defaults to the rentable-only
   * rental picker; pass "/api/pickable-assets" to show every asset
   * regardless of its "可租借" flag (e.g. maintenance/disposal requests). */
  endpoint?: string;
}

const statusConfig: Record<string, { label: string; color: string }> = {
  available:   { label: "可用",   color: "badge-success" },
  rented:      { label: "借出",   color: "badge-warning" },
  faulty:      { label: "故障",   color: "badge-error" },
  broken:      { label: "故障",   color: "badge-error" },
  repairing:   { label: "維修中", color: "badge-info" },
  lost:        { label: "遺失",   color: "badge-error" },
  retired:     { label: "報廢",   color: "badge-ghost" },
  transferred: { label: "移撥",   color: "badge-ghost" },
};

export function AssetPicker({ selected, onChange, showFilters, endpoint = "/api/rental-pickable-assets" }: AssetPickerProps) {
  const { t } = useTranslation();
  const [assets, setAssets] = useState<AssetItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [categories, setCategories] = useState<CategoryOption[]>([]);
  const [categoryFilter, setCategoryFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState(showFilters ? "available" : "");
  const [sourceFilter, setSourceFilter] = useState<"" | "mdm" | "standalone">("");
  const [quickQty, setQuickQty] = useState("");

  useEffect(() => {
    setLoading(true);
    apiClient.get(endpoint)
      .then(({ data }) => setAssets(data.assets || []))
      .catch((err) => console.error("AssetPicker load:", err))
      .finally(() => setLoading(false));
  }, [endpoint]);

  useEffect(() => {
    if (showFilters) {
      apiClient.get("/api/categories").then(({ data }) => setCategories(data.categories || [])).catch(() => {});
    }
  }, [showFilters]);

  const filtered = useMemo(() => {
    let result = assets;
    if (search) {
      const q = search.toLowerCase();
      result = result.filter(
        (a) =>
          a.name.toLowerCase().includes(q) ||
          a.asset_number.toLowerCase().includes(q) ||
          a.serial_number.toLowerCase().includes(q) ||
          (a.device_udid || "").toLowerCase().includes(q) ||
          a.model.toLowerCase().includes(q) ||
          a.spec.toLowerCase().includes(q)
      );
    }
    if (statusFilter) result = result.filter((a) => a.asset_status === statusFilter);
    if (categoryFilter) result = result.filter((a) => a.category_id === categoryFilter);
    if (sourceFilter === "mdm") result = result.filter((a) => a.device_udid);
    else if (sourceFilter === "standalone") result = result.filter((a) => !a.device_udid);
    return result;
  }, [assets, search, statusFilter, categoryFilter, sourceFilter]);

  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const toggle = (id: string) => {
    if (selectedSet.has(id)) {
      onChange(selected.filter((x) => x !== id));
    } else {
      onChange([...selected, id]);
    }
  };

  const selectAll = () => {
    const allIds = filtered.map((a) => a.asset_id);
    const allSelected = allIds.every((id) => selectedSet.has(id));
    if (allSelected) {
      onChange(selected.filter((id) => !allIds.includes(id)));
    } else {
      const merged = new Set([...selected, ...allIds]);
      onChange([...merged]);
    }
  };

  const clearAll = () => onChange([]);

  const handleQuickQty = () => {
    const qty = parseInt(quickQty);
    if (isNaN(qty) || qty <= 0) return;
    const unselected = filtered.filter((a) => !selectedSet.has(a.asset_id));
    const toSelect = unselected.slice(0, qty).map((a) => a.asset_id);
    onChange([...selected, ...toSelect]);
    setQuickQty("");
  };

  const selectedAssets = assets.filter((a) => selectedSet.has(a.asset_id));

  const statusCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const a of assets) counts[a.asset_status] = (counts[a.asset_status] || 0) + 1;
    return counts;
  }, [assets]);

  return (
    <div className="space-y-3">
      {/* Selected tags */}
      {selectedAssets.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selectedAssets.map((a) => (
            <span key={a.asset_id} className="badge badge-primary gap-1">
              {a.name || a.asset_number || a.serial_number}
              <button onClick={() => toggle(a.asset_id)} className="hover:opacity-70"><X size={12} /></button>
            </span>
          ))}
          <button onClick={clearAll} className="badge badge-ghost gap-1 cursor-pointer hover:badge-error">
            {t("devicePicker.clearAll")}
          </button>
        </div>
      )}

      {/* Search + quick qty */}
      <div className="flex gap-2 flex-wrap">
        <label className="input input-bordered input-sm flex items-center gap-2 flex-1 min-w-48">
          <Search size={14} className="opacity-50" />
          <input type="text" placeholder={t("devicePicker.placeholder")} value={search} onChange={(e) => setSearch(e.target.value)} className="grow" />
        </label>
        <div className="join">
          <input
            type="number"
            min="1"
            placeholder="數量"
            value={quickQty}
            onChange={(e) => setQuickQty(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleQuickQty()}
            className="input input-bordered input-sm join-item w-20"
          />
          <button onClick={handleQuickQty} disabled={!quickQty} className="btn btn-sm join-item gap-1">
            <Hash size={12} /> 快選
          </button>
        </div>
        <button onClick={selectAll} className="btn btn-ghost btn-sm">{t("devicePicker.selectAll")}</button>
      </div>

      {/* Status filter pills + Source + Category */}
      {showFilters && (
        <div className="space-y-2">
          <div className="flex gap-1 flex-wrap">
            <button
              onClick={() => setStatusFilter("")}
              className={`btn btn-xs gap-1 ${statusFilter === "" ? "btn-neutral" : "btn-ghost"}`}
            >
              全部 <span className="badge badge-xs">{assets.length}</span>
            </button>
            {Object.entries(statusConfig).map(([key, cfg]) => {
              const count = statusCounts[key] || 0;
              if (count === 0 && key !== "available") return null;
              return (
                <button
                  key={key}
                  onClick={() => setStatusFilter(statusFilter === key ? "" : key)}
                  className={`btn btn-xs gap-1 ${statusFilter === key ? "btn-neutral" : "btn-ghost"}`}
                >
                  <span className={`badge badge-xs ${cfg.color}`}></span>
                  {cfg.label}
                  <span className="badge badge-xs">{count}</span>
                </button>
              );
            })}
          </div>
          <div className="flex gap-1 flex-wrap">
            <button
              onClick={() => setSourceFilter("")}
              className={`btn btn-xs gap-1 ${sourceFilter === "" ? "btn-neutral" : "btn-ghost"}`}
            >
              全部來源
            </button>
            <button
              onClick={() => setSourceFilter(sourceFilter === "mdm" ? "" : "mdm")}
              className={`btn btn-xs gap-1 ${sourceFilter === "mdm" ? "btn-neutral" : "btn-ghost"}`}
            >
              <Tablet size={10} /> MDM 裝置
            </button>
            <button
              onClick={() => setSourceFilter(sourceFilter === "standalone" ? "" : "standalone")}
              className={`btn btn-xs gap-1 ${sourceFilter === "standalone" ? "btn-neutral" : "btn-ghost"}`}
            >
              <Package size={10} /> 獨立資產
            </button>
          </div>
          {categories.length > 0 && (
            <select value={categoryFilter} onChange={(e) => setCategoryFilter(e.target.value)} className="select select-bordered select-sm w-full">
              <option value="">全部分類</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>{"\u3000".repeat(c.level)}{c.name}</option>
              ))}
            </select>
          )}
        </div>
      )}

      {/* Summary */}
      {selected.length > 0 && (
        <div className="text-sm font-medium text-primary">
          {t("devicePicker.selected", { count: selected.length })}
        </div>
      )}

      {/* Asset list */}
      <div className="border border-base-300 rounded-lg max-h-56 overflow-y-auto">
        {loading ? (
          <div className="flex items-center justify-center py-6">
            <span className="loading loading-spinner loading-sm mr-2"></span>
            {t("devicePicker.loading")}
          </div>
        ) : filtered.length === 0 ? (
          <div className="text-center py-6 text-base-content/50 text-sm">{t("devicePicker.noResults")}</div>
        ) : (
          <ul className="divide-y divide-base-200">
            {filtered.map((a) => {
              const isSelected = selectedSet.has(a.asset_id);
              const sc = statusConfig[a.asset_status] || statusConfig.available;
              const Icon = a.device_udid ? Tablet : Package;
              const primary = a.name || a.asset_number || a.serial_number || a.asset_id;
              const secondary = [a.asset_number, a.serial_number, a.model, a.os_version].filter(Boolean).join(" · ");
              return (
                <li
                  key={a.asset_id}
                  onClick={() => toggle(a.asset_id)}
                  className={`flex items-center gap-3 px-3 py-2 cursor-pointer hover:bg-base-200 transition-colors ${isSelected ? "bg-primary/5" : ""}`}
                >
                  <div className={`w-5 h-5 rounded border flex items-center justify-center flex-shrink-0 ${isSelected ? "bg-primary border-primary text-primary-content" : "border-base-300"}`}>
                    {isSelected && <Check size={12} />}
                  </div>
                  <Icon size={16} className="opacity-40 flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium truncate">{primary}</div>
                    <div className="text-xs opacity-50 truncate">{secondary || "-"}</div>
                    {a.last_return_location && (
                      <div className="text-xs text-info flex items-center gap-1 truncate">
                        <MapPin size={10} className="flex-shrink-0" />
                        {a.last_return_location.address
                          ? a.last_return_location.address
                          : a.last_return_location.lat != null ? (
                            <a
                              href={`https://maps.google.com/?q=${a.last_return_location.lat},${a.last_return_location.lng}`}
                              target="_blank"
                              rel="noreferrer"
                              onClick={(e) => e.stopPropagation()}
                              className="link"
                            >
                              最後位置（開啟地圖）
                            </a>
                          ) : "最後位置"}
                        {a.last_return_at && <span className="opacity-60">（{new Date(a.last_return_at).toLocaleDateString()}）</span>}
                      </div>
                    )}
                  </div>
                  {!a.device_udid && (
                    <span className="badge badge-outline badge-xs flex-shrink-0">獨立</span>
                  )}
                  <span className={`badge badge-xs flex-shrink-0 ${sc.color}`}>{sc.label}</span>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
