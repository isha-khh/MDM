import { useEffect, useMemo, useState } from "react";
import { useAuthStore } from "../stores/authStore";
import { useDeviceStore, type DeviceRow } from "../stores/deviceStore";
import { useTranslation } from "react-i18next";
import { useDialog } from "../components/DialogProvider";
import { Link, useNavigate } from "react-router-dom";
import { Search, RefreshCw, Send, Info, X, Filter, Download, Cloud, Zap } from "lucide-react";
import type { ColDef, ICellRendererParams, RowSelectionOptions } from "ag-grid-enterprise";
import apiClient from "../lib/apiClient";
import { DataGrid } from "../components/DataGrid";

const ASSET_STATUS_CONFIG: Record<string, { label: string; badge: string }> = {
  available:    { label: "可用",   badge: "badge-success" },
  rented:       { label: "借出",   badge: "badge-warning" },
  faulty:       { label: "故障",   badge: "badge-error" },
  repairing:    { label: "維修中", badge: "badge-info" },
  lost:         { label: "遺失",   badge: "badge-error" },
  retired:      { label: "報廢",   badge: "badge-ghost" },
  transferred:  { label: "移撥",   badge: "badge-ghost" },
};

interface CategoryOption { id: string; name: string; level: number; parent_id: string | null; }
interface UserOption { id: string; username: string; display_name: string; }

export function Devices() {
  const { t } = useTranslation();
  const dialog = useDialog();
  const { clients, user } = useAuthStore();
  const isAdmin = user?.role === "admin";
  const {
    devices, total, loading, filters,
    setFilter, clearFilters, loadDevices,
    selected, setSelected,
  } = useDeviceStore();
  const navigate = useNavigate();
  const [syncing, setSyncing] = useState(false);
  const [syncingInfo, setSyncingInfo] = useState(false);
  const [syncingDEP, setSyncingDEP] = useState(false);
  const [applyingDEP, setApplyingDEP] = useState(false);
  const [categories, setCategories] = useState<CategoryOption[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);

  // Load filter options
  useEffect(() => {
    apiClient.get("/api/categories").then(({ data }) => setCategories(data.categories || [])).catch(() => {});
    apiClient.get("/api/users-list").then(({ data }) => setUsers(data.users || [])).catch(() => {});
  }, []);

  // Initial load
  useEffect(() => { loadDevices(); }, []);

  const handleSync = async () => {
    if (!clients) return;
    setSyncing(true);
    try {
      const resp = await clients.device.syncDevices({});
      await dialog.success(t("devices.syncSuccess", { count: resp.syncedCount }));
      loadDevices();
    } catch (err) {
      await dialog.error(t("devices.syncFailed") + ": " + (err instanceof Error ? err.message : ""));
    } finally { setSyncing(false); }
  };

  const handleSyncInfo = async () => {
    setSyncingInfo(true);
    try {
      const { data } = await apiClient.post("/api/sync-device-info");
      await dialog.success(t("assets.syncInfo") + `: ${data.count} devices`);
    } catch (err) {
      await dialog.error("Sync failed: " + (err instanceof Error ? err.message : ""));
    } finally { setSyncingInfo(false); }
  };

  // Trigger MicroMDM's DEP sync (pulls fresh assignments from Apple). Useful
  // when an admin just registered a new Mac in ABM and doesn't want to wait
  // for the next scheduled sync.
  const handleSyncDEP = async () => {
    if (!clients) return;
    setSyncingDEP(true);
    try {
      await clients.device.syncDEPDevices({});
      await dialog.success("已觸發 DEP 同步，MicroMDM 正在跟 Apple 拉資料，請稍候幾秒再按「同步」更新清單。");
    } catch (err) {
      await dialog.error("DEP 同步失敗：" + (err instanceof Error ? err.message : ""));
    } finally {
      setSyncingDEP(false);
    }
  };

  // Run the DEP profile auto-assigner one cycle right now. Equivalent to
  // waiting for the next scheduler tick but immediate. Server returns 503
  // if DEP_AUTO_ASSIGN=false (then there's nothing to trigger).
  const handleApplyDEP = async () => {
    setApplyingDEP(true);
    try {
      await apiClient.post("/api/dep/apply-now");
      await dialog.success("DEP 套用已執行，請看伺服器 log 取得每台裝置的明細");
    } catch (err: unknown) {
      let detail = "";
      if (err && typeof err === "object" && "response" in err) {
        const resp = (err as { response?: { status?: number; data?: { error?: string } } }).response;
        if (resp?.data?.error) detail = resp.data.error;
        else if (resp?.status) detail = `HTTP ${resp.status}`;
      } else if (err instanceof Error) {
        detail = err.message;
      }
      await dialog.error("立即套用失敗：" + (detail || "(未知)"));
    } finally {
      setApplyingDEP(false);
    }
  };

  // Build category options with indentation
  const categoryOptions = categories.map((c) => ({
    ...c,
    label: "\u00A0\u00A0".repeat(c.level) + c.name,
  }));

  const hasFilters = filters.categoryId || filters.custodianId;

  const rowSelection = useMemo<RowSelectionOptions<DeviceRow>>(() => ({
    mode: "multiRow",
    checkboxes: true,
    headerCheckbox: true,
    enableClickSelection: false,
  }), []);

  const columnDefs = useMemo<ColDef<DeviceRow>[]>(() => [
    {
      headerName: t("devices.name"),
      field: "device_name",
      minWidth: 160,
      cellRenderer: (p: ICellRendererParams<DeviceRow>) =>
        <div className="font-medium text-primary">{p.value || "-"}</div>,
    },
    { headerName: t("devices.serial"), field: "serial_number", width: 160, cellClass: "font-mono text-xs" },
    {
      headerName: "分類",
      field: "category_name",
      width: 140,
      cellRenderer: (p: ICellRendererParams<DeviceRow>) =>
        p.value
          ? <span className="badge badge-ghost badge-sm">{p.value}</span>
          : <span className="opacity-30">-</span>,
    },
    {
      headerName: "保管人",
      field: "custodian_name",
      width: 140,
      valueFormatter: (p) => p.value || "-",
    },
    {
      headerName: "裝置狀態",
      field: "asset_status",
      width: 110,
      cellRenderer: (p: ICellRendererParams<DeviceRow>) => {
        const st = ASSET_STATUS_CONFIG[p.value as string] || ASSET_STATUS_CONFIG.available;
        return <span className={`badge badge-sm ${st.badge}`}>{st.label}</span>;
      },
    },
    { headerName: t("devices.model"), field: "model", width: 140, cellClass: "text-sm opacity-70", valueFormatter: (p) => p.value || "-" },
    { headerName: t("devices.os"), field: "os_version", width: 140, cellClass: "text-sm", valueFormatter: (p) => p.value || "-" },
    {
      headerName: t("devices.lastSeen"),
      field: "last_seen",
      width: 180,
      cellClass: "text-sm opacity-70",
      valueFormatter: (p) => p.value ? new Date(p.value as string).toLocaleString() : "-",
    },
    {
      headerName: t("common.status"),
      field: "enrollment_status",
      width: 120,
      cellRenderer: (p: ICellRendererParams<DeviceRow>) =>
        <span className={`badge badge-sm ${p.value === "enrolled" ? "badge-success" : "badge-ghost"}`}>{p.value}</span>,
    },
  ], [t]);

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">{t("devices.title")}</h1>
          <p className="text-sm text-base-content/60">{t("devices.count", { count: total })}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <label className="input input-bordered input-sm flex items-center gap-2" data-tour="device-search">
            <Search size={14} className="opacity-50" />
            <input
              type="text"
              placeholder={t("common.search")}
              value={filters.search}
              onChange={(e) => setFilter("search", e.target.value)}
              className="grow w-32"
            />
          </label>
          {isAdmin && (
            <>
              <div className="tooltip tooltip-bottom" data-tip="觸發 MicroMDM 跟 Apple ABM 拉新指派。新加 Mac 不想等排程時用">
                <button onClick={handleSyncDEP} disabled={syncingDEP} className="btn btn-warning btn-sm gap-1">
                  {syncingDEP ? <span className="loading loading-spinner loading-xs"></span> : <Cloud size={14} />}
                  從 ABM 同步 DEP
                </button>
              </div>
              <div className="tooltip tooltip-bottom" data-tip="立即跑一次排程：依 productFamily 把新機自動套上對應 DEP profile（不用等 5 分鐘）">
                <button onClick={handleApplyDEP} disabled={applyingDEP} className="btn btn-warning btn-sm gap-1">
                  {applyingDEP ? <span className="loading loading-spinner loading-xs"></span> : <Zap size={14} />}
                  立即套用 DEP profile
                </button>
              </div>
            </>
          )}
          <button onClick={handleSync} disabled={syncing} className="btn btn-success btn-sm gap-1">
            {syncing ? <span className="loading loading-spinner loading-xs"></span> : <RefreshCw size={14} />}
            {t("devices.sync")}
          </button>
          <button onClick={handleSyncInfo} disabled={syncingInfo} className="btn btn-info btn-sm gap-1">
            {syncingInfo ? <span className="loading loading-spinner loading-xs"></span> : <Info size={14} />}
            {t("assets.syncInfo")}
          </button>
          <button onClick={() => window.open("/api/assets-export", "_blank")} className="btn btn-outline btn-sm gap-1">
            <Download size={14} />Excel
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-2 items-center">
        <Filter size={14} className="opacity-50" />
        <select
          value={filters.categoryId}
          onChange={(e) => setFilter("categoryId", e.target.value)}
          className="select select-bordered select-sm"
        >
          <option value="">全部分類</option>
          {categoryOptions.map((c) => (
            <option key={c.id} value={c.id}>{c.label}</option>
          ))}
        </select>
        <select
          value={filters.custodianId}
          onChange={(e) => setFilter("custodianId", e.target.value)}
          className="select select-bordered select-sm"
        >
          <option value="">全部保管人</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>{u.display_name || u.username}</option>
          ))}
        </select>
        {hasFilters && (
          <button onClick={clearFilters} className="btn btn-ghost btn-sm gap-1">
            <X size={14} /> 清除篩選
          </button>
        )}
      </div>

      {/* Selection bar */}
      {selected.size > 0 && (
        <div role="alert" className="alert alert-info">
          <span className="font-medium">{t("common.selected", { count: selected.size })}</span>
          <Link to={`/commands?udids=${Array.from(selected).join(",")}`} className="btn btn-sm btn-primary gap-1">
            <Send size={14} />{t("devices.sendCommand")}
          </Link>
        </div>
      )}

      {/* Grid */}
      <div className="card bg-base-100 shadow p-2" data-tour="device-table">
        <DataGrid<DeviceRow>
          rowData={devices}
          columnDefs={columnDefs}
          loading={loading}
          rowSelection={rowSelection}
          getRowId={(p) => p.data.udid}
          overlayNoRowsTemplate={`<span class="opacity-50">${t("devices.noDevices")}</span>`}
          onSelectionChanged={(e) => {
            const udids = e.api.getSelectedRows().map((r) => r.udid);
            setSelected(udids);
          }}
          onRowDoubleClicked={(e) => {
            if (e.data) navigate(`/mdm/devices/${e.data.udid}`);
          }}
        />
      </div>
    </div>
  );
}
