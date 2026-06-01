import { useState, useEffect, useCallback } from "react";
import { useParams, Link } from "react-router-dom";
import { useAuthStore } from "../stores/authStore";
import { useEventStore } from "../stores/eventStore";
import { useTranslation } from "react-i18next";
import { useDialog } from "../components/DialogProvider";
import { ResponseViewer } from "../components/ResponseViewer";
import {
  ArrowLeft, RefreshCw, Smartphone, Lock, RotateCcw, Power,
  MapPin, Volume2, Bell, Info, AppWindow, FileText, Shield, Award,
  Package, MapPinOff, Battery, Wifi, HardDrive, KeyRound, Trash2,
  Download, PackageMinus, Upload, ClipboardList, RefreshCcw,
} from "lucide-react";
import { ProfilePicker } from "../components/ProfilePicker";
import { AssetForm } from "../components/AssetForm";
import apiClient from "../lib/apiClient";
import {
  COMMAND_SPECS, type MDMCommandSpec, type Platform,
  inferPlatform, checkCommand, dialogRequired,
} from "../lib/mdmCommands";

interface ManagedApp {
  id: string;
  name: string;
  bundle_id: string;
  app_type: "vpp" | "enterprise";
  itunes_store_id: string;
  manifest_url: string;
  purchased_qty: number;
  installed_count: number;
  icon_url: string;
}

interface DeviceApp {
  id: string;
  app_id: string;
  app_name: string;
  bundle_id: string;
  app_type: string;
  installed_at: string;
}

interface DeviceData {
  udid: string;
  serial_number: string;
  device_name: string;
  model: string;
  os_version: string;
  last_seen: string;
  enrollment_status: string;
  is_supervised: boolean;
  is_lost_mode: boolean;
  battery_level: number;
  details: Record<string, unknown>;
}

// --- Tab definitions ---
interface TabDef {
  key: string;
  label: string;
  icon: React.ReactNode;
  method: string;        // gRPC command method to sync
  detailsKey: string;    // key in device.details for cached base64 plist
  updatedKey: string;    // key in device.details for last updated time
}

const tabs: TabDef[] = [
  { key: "info",     label: "概覽",     icon: <Info size={16} />,     method: "getDeviceInfo",         detailsKey: "device_info",         updatedKey: "device_info" },
  { key: "apps",     label: "已裝 App", icon: <AppWindow size={16} />, method: "getInstalledApps",      detailsKey: "installed_apps_raw",  updatedKey: "installed_apps_updated" },
  { key: "profiles", label: "描述檔",   icon: <FileText size={16} />,  method: "getProfileList",        detailsKey: "profiles_raw",        updatedKey: "profiles_updated" },
  { key: "security", label: "安全性",   icon: <Shield size={16} />,    method: "getSecurityInfo",       detailsKey: "security_raw",        updatedKey: "security_updated" },
  { key: "certs",    label: "憑證",     icon: <Award size={16} />,     method: "getCertificateList",    detailsKey: "certs_raw",           updatedKey: "certs_updated" },
  { key: "updates",  label: "更新",     icon: <Package size={16} />,   method: "getAvailableOSUpdates", detailsKey: "updates_raw",         updatedKey: "updates_updated" },
];

// --- Action command definitions ---
// Icon mapping (kept in this file because lucide JSX can't live in the spec library).
const COMMAND_ICONS: Record<string, React.ReactNode> = {
  sendPush:          <Bell size={14} />,
  lockDevice:        <Lock size={14} />,
  restartDevice:     <RotateCcw size={14} />,
  shutdownDevice:    <Power size={14} />,
  clearPasscode:     <KeyRound size={14} />,
  enableLostMode:    <MapPin size={14} />,
  disableLostMode:   <MapPinOff size={14} />,
  getDeviceLocation: <MapPin size={14} />,
  playLostModeSound: <Volume2 size={14} />,
  showInstallApp:    <Download size={14} />,
  showUpdateApp:     <RefreshCcw size={14} />,
  showUninstallApp:  <PackageMinus size={14} />,
  eraseDevice:       <Trash2 size={14} />,
};

export function DeviceDetail() {
  const { t } = useTranslation();
  const dialog = useDialog();
  const { udid } = useParams<{ udid: string }>();
  const { clients, user: currentUser } = useAuthStore();
  const { trackCommand, events } = useEventStore();
  const userRole = currentUser?.role || "viewer";
  const [device, setDevice] = useState<DeviceData | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState("info");
  const [syncing, setSyncing] = useState<string | null>(null);
  const [executing, setExecuting] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<string | null>(null);
  const [showInstallProfile, setShowInstallProfile] = useState(false);
  const [selectedProfileId, setSelectedProfileId] = useState("");
  const [selectedProfilePayload, setSelectedProfilePayload] = useState("");
  const [showLostMode, setShowLostMode] = useState(false);
  const [lostModeMessage, setLostModeMessage] = useState("");
  const [lostModePhone, setLostModePhone] = useState("");
  const [lostModeFootnote, setLostModeFootnote] = useState("");
  const [showAppInstall, setShowAppInstall] = useState(false);
  const [showAppUninstall, setShowAppUninstall] = useState(false);
  const [managedApps, setManagedApps] = useState<ManagedApp[]>([]);
  const [deviceApps, setDeviceApps] = useState<DeviceApp[]>([]);
  const [selectedInstallIds, setSelectedInstallIds] = useState<string[]>([]);
  const [selectedUpdateIds, setSelectedUpdateIds] = useState<string[]>([]);
  const [selectedUninstallIds, setSelectedUninstallIds] = useState<string[]>([]);
  const [appLoading, setAppLoading] = useState(false);
  const [showAppUpdate, setShowAppUpdate] = useState(false);
  // PIN dialogs — Apple requires a 6-digit PIN for macOS DeviceLock and EraseDevice.
  const [pinDialog, setPinDialog] = useState<null | "lock" | "erase">(null);
  const [pinValue, setPinValue] = useState("");
  const [lockMessage, setLockMessage] = useState("");

  const baseUrl = import.meta.env.DEV ? "" : window.location.origin;

  const loadDevice = useCallback(async () => {
    if (!udid) return;
    setLoading(true);
    try {
      const resp = await fetch(`${baseUrl}/api/devices/${udid}`, {
        credentials: "include",
      });
      if (resp.ok) setDevice(await resp.json());
    } catch (err) { console.error("Load device:", err); }
    finally { setLoading(false); }
  }, [udid, baseUrl]);

  useEffect(() => { loadDevice(); }, [loadDevice]);

  // Auto-refresh from DB when new acknowledge events arrive for this device
  const deviceEventCount = events.filter((e) => e.udid === udid && e.eventType === "acknowledge").length;
  useEffect(() => {
    if (deviceEventCount > 0) {
      const timer = setTimeout(loadDevice, 1500);
      return () => clearTimeout(timer);
    }
  }, [deviceEventCount]);

  // Sync a specific tab (send query command)
  const syncTab = async (tab: TabDef) => {
    if (!clients || !udid) return;
    setSyncing(tab.key);
    try {
      // @ts-expect-error dynamic method call
      const resp = await clients.command[tab.method]({ udids: [udid] });
      trackCommand(tab.label, [udid], resp.commandUuid);
    } catch (err) {
      console.error("Sync failed:", err);
    } finally {
      setSyncing(null);
    }
  };

  // Sync ALL tabs at once
  const syncAll = async () => {
    if (!clients || !udid) return;
    setSyncing("all");
    for (const tab of tabs) {
      try {
        // @ts-expect-error dynamic method call
        const resp = await clients.command[tab.method]({ udids: [udid] });
        trackCommand(tab.label, [udid], resp.commandUuid);
      } catch (err) { console.error(`Sync ${tab.key}:`, err); }
    }
    setSyncing(null);
  };

  // Derive platform once per render — used by command spec checks below.
  const devicePlatform: Platform = device
    ? inferPlatform(device.model || "")
    : "unknown";

  // Execute an action command via its spec. The spec decides whether to open
  // a parameter dialog first or call the backend directly.
  const executeAction = async (cmd: MDMCommandSpec) => {
    if (!clients || !udid) return;

    // Dialog-routed commands: open the right modal and let its handler do the
    // actual send (so the user can input PIN / message / app selection).
    if (cmd.dialog && dialogRequired(cmd, devicePlatform)) {
      switch (cmd.dialog) {
        case "lost-mode":    setShowLostMode(true); return;
        case "install-app":  openAppInstall(); return;
        case "update-app":   openAppUpdate(); return;
        case "uninstall-app": openAppUninstall(); return;
        case "lock-pin":     setPinValue(""); setLockMessage(""); setPinDialog("lock"); return;
        case "erase-pin":    setPinValue(""); setPinDialog("erase"); return;
      }
    }

    // Direct-send commands (or platforms where the dialog isn't needed,
    // e.g. iOS DeviceLock has no PIN requirement).
    if (cmd.danger && !(await dialog.confirm(`確定要執行「${cmd.label}」嗎？此操作無法復原。`))) return;
    setExecuting(cmd.method);
    setActionResult(null);
    try {
      // @ts-expect-error dynamic method call
      const resp = await clients.command[cmd.method]({ udids: [udid] });
      trackCommand(cmd.label, [udid], resp.commandUuid);
      if (resp.rawResponse) setActionResult(resp.rawResponse);
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally { setExecuting(null); }
  };

  // Confirm + send a macOS DeviceLock with the 6-digit PIN the user typed.
  const executePinLock = async () => {
    if (!clients || !udid) return;
    if (!/^\d{6}$/.test(pinValue)) return; // form validation prevents this, defensive
    setPinDialog(null);
    setExecuting("lockDevice");
    setActionResult(null);
    try {
      const resp = await clients.command.lockDevice({
        udids: [udid],
        pin: pinValue,
        message: lockMessage,
      });
      trackCommand("鎖定裝置 (PIN)", [udid], resp.commandUuid);
      if (resp.rawResponse) setActionResult(resp.rawResponse);
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally {
      setExecuting(null);
      setPinValue("");
      setLockMessage("");
    }
  };

  // Confirm + send a macOS EraseDevice with the 6-digit PIN.
  const executePinErase = async () => {
    if (!clients || !udid) return;
    if (!/^\d{6}$/.test(pinValue)) return;
    if (!(await dialog.confirm(`確定要清除這台 Mac 嗎？此操作無法復原。`))) return;
    setPinDialog(null);
    setExecuting("eraseDevice");
    setActionResult(null);
    try {
      const resp = await clients.command.eraseDevice({
        udids: [udid],
        pin: pinValue,
      });
      trackCommand("清除裝置 (PIN)", [udid], resp.commandUuid);
      if (resp.rawResponse) setActionResult(resp.rawResponse);
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally {
      setExecuting(null);
      setPinValue("");
    }
  };

  const executeLostMode = async () => {
    if (!clients || !udid || !lostModeMessage) return;
    setShowLostMode(false);
    setExecuting("enableLostMode");
    setActionResult(null);
    try {
      const resp = await clients.command.enableLostMode({
        udids: [udid],
        message: lostModeMessage,
        phoneNumber: lostModePhone,
        footnote: lostModeFootnote,
      });
      trackCommand("啟用遺失模式", [udid], resp.commandUuid);
      if (resp.rawResponse) setActionResult(resp.rawResponse);
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally {
      setExecuting(null);
      setLostModeMessage("");
      setLostModePhone("");
      setLostModeFootnote("");
    }
  };

  // Remove a profile by identifier
  const removeProfile = async (identifier: string) => {
    if (!clients || !udid) return;
    setExecuting("removeProfile");
    try {
      const resp = await clients.command.removeProfile({ udids: [udid], identifier });
      trackCommand("移除描述檔", [udid], resp.commandUuid);
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally { setExecuting(null); }
  };

  // Install a profile
  const installProfile = async () => {
    if (!clients || !udid || !selectedProfilePayload) return;
    setShowInstallProfile(false);
    setExecuting("installProfile");
    try {
      const binary = atob(selectedProfilePayload);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      const resp = await clients.command.installProfile({ udids: [udid], payload: bytes });
      trackCommand("安裝描述檔", [udid], resp.commandUuid);
      setSelectedProfileId("");
      setSelectedProfilePayload("");
    } catch (err) {
      setActionResult(`Error: ${err instanceof Error ? err.message : "Unknown"}`);
    } finally { setExecuting(null); }
  };

  // Load managed apps list & device-installed apps
  const loadManagedApps = useCallback(async () => {
    try {
      const { data } = await apiClient.get("/api/managed-apps");
      setManagedApps(data.apps || []);
    } catch (err) { console.error("Load managed apps:", err); }
  }, []);


  const loadDeviceApps = useCallback(async () => {
    if (!udid) return;
    try {
      const { data } = await apiClient.get(`/api/device-apps?device_udid=${udid}`);
      setDeviceApps(data.device_apps || []);
    } catch (err) { console.error("Load device apps:", err); }
  }, [udid]);

  useEffect(() => { if (activeTab === "apps") { loadManagedApps(); loadDeviceApps(); } }, [activeTab, loadManagedApps, loadDeviceApps]);

  const openAppInstall = async () => {
    await Promise.all([loadManagedApps(), loadDeviceApps()]);
    setSelectedInstallIds([]);
    setShowAppInstall(true);
  };

  const openAppUpdate = async () => {
    await Promise.all([loadManagedApps(), loadDeviceApps()]);
    setSelectedUpdateIds([]);
    setShowAppUpdate(true);
  };

  const openAppUninstall = async () => {
    await Promise.all([loadManagedApps(), loadDeviceApps()]);
    setSelectedUninstallIds([]);
    setShowAppUninstall(true);
  };

  const handleInstallApp = async () => {
    if (selectedInstallIds.length === 0 || !udid) return;
    setAppLoading(true);
    setActionResult(null);
    try {
      for (const appId of selectedInstallIds) {
        const { data } = await apiClient.post("/api/device-apps/install", {
          app_id: appId,
          udid,
        });
        if (data.command_uuid) {
          const app = managedApps.find((a) => a.id === appId);
          trackCommand(`安裝 ${app?.name || "App"}`, [udid], data.command_uuid);
        }
      }
      setShowAppInstall(false);
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error
        || (err instanceof Error ? err.message : "Unknown error");
      setActionResult(`Error: ${msg}`);
    } finally { setAppLoading(false); }
  };

  const handleUninstallApp = async () => {
    if (selectedUninstallIds.length === 0 || !udid) return;
    setAppLoading(true);
    setActionResult(null);
    try {
      for (const appId of selectedUninstallIds) {
        const { data } = await apiClient.post("/api/device-apps/uninstall", {
          app_id: appId,
          udid,
        });
        if (data.command_uuid) {
          const app = deviceApps.find((a) => a.app_id === appId);
          trackCommand(`移除 ${app?.app_name || "App"}`, [udid], data.command_uuid);
        }
      }
      setShowAppUninstall(false);
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error
        || (err instanceof Error ? err.message : "Unknown error");
      setActionResult(`Error: ${msg}`);
    } finally { setAppLoading(false); }
  };

  const handleUpdateApp = async () => {
    if (selectedUpdateIds.length === 0 || !udid) return;
    setAppLoading(true);
    setActionResult(null);
    try {
      for (const appId of selectedUpdateIds) {
        const { data } = await apiClient.post("/api/device-apps/update", {
          app_id: appId,
          udid,
        });
        if (data.command_uuid) {
          const app = deviceApps.find((a) => a.app_id === appId);
          trackCommand(`更新 ${app?.app_name || "App"}`, [udid], data.command_uuid);
        }
      }
      setShowAppUpdate(false);
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error
        || (err instanceof Error ? err.message : "Unknown error");
      setActionResult(`Error: ${msg}`);
    } finally { setAppLoading(false); }
  };

  // Get cached payload for the active tab
  const currentTab = tabs.find((t) => t.key === activeTab);
  const getCachedPayload = (): string | null => {
    if (!currentTab || !device?.details) return null;
    const raw = device.details[currentTab.detailsKey];
    if (typeof raw === "string") return raw;
    if (typeof raw === "object" && raw !== null) return JSON.stringify(raw, null, 2);
    return null;
  };
  const getUpdatedAt = (): string | null => {
    if (!currentTab || !device?.details) return null;
    const t = device.details[currentTab.updatedKey];
    if (typeof t === "string" && t.includes("T")) return new Date(t).toLocaleString();
    if (typeof t === "object" && t !== null) {
      // device_info is an object, check updated_at inside
      const obj = t as Record<string, unknown>;
      if (typeof obj.updated_at === "string") return new Date(obj.updated_at).toLocaleString();
    }
    return null;
  };

  if (loading) {
    return <div className="flex justify-center py-12"><span className="loading loading-spinner loading-lg"></span></div>;
  }
  if (!device) {
    return (
      <div className="text-center py-12">
        <p className="text-base-content/50">Device not found</p>
        <Link to="/devices" className="btn btn-ghost btn-sm mt-4"><ArrowLeft size={14} /> {t("devices.title")}</Link>
      </div>
    );
  }

  const batteryPercent = device.battery_level >= 0 ? Math.round(device.battery_level * 100) : null;
  const cachedPayload = getCachedPayload();
  const updatedAt = getUpdatedAt();

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Link to="/devices" className="btn btn-ghost btn-sm btn-circle"><ArrowLeft size={18} /></Link>
        <div className="flex-1">
          <h1 className="text-2xl font-bold flex items-center gap-2">
            <Smartphone size={24} />
            {device.device_name || device.serial_number || udid}
          </h1>
          <p className="text-sm text-base-content/60 font-mono">{udid}</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <span className={`badge ${device.enrollment_status === "enrolled" ? "badge-success" : "badge-ghost"}`}>{device.enrollment_status}</span>
          {device.is_lost_mode && <span className="badge badge-error gap-1"><MapPin size={12} /> 遺失模式</span>}
          {device.is_supervised && <span className="badge badge-info gap-1"><Shield size={12} /> 受監管</span>}
        </div>
        <button onClick={syncAll} disabled={syncing !== null} className="btn btn-primary btn-sm gap-1">
          {syncing === "all" ? <span className="loading loading-spinner loading-xs"></span> : <RefreshCw size={14} />}
          全部同步
        </button>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        {[
          { icon: <Smartphone size={14} />, label: t("devices.serial"), value: device.serial_number },
          { icon: <HardDrive size={14} />, label: t("devices.model"), value: device.model },
          { icon: <Package size={14} />, label: t("devices.os"), value: device.os_version },
          { icon: <Wifi size={14} />, label: t("devices.lastSeen"), value: device.last_seen ? new Date(device.last_seen).toLocaleString() : "-" },
          ...(batteryPercent !== null ? [{ icon: <Battery size={14} />, label: "電量", value: `${batteryPercent}%` }] : []),
        ].map((item) => (
          <div key={item.label} className="stat bg-base-100 rounded-box shadow p-3">
            <div className="stat-title text-xs flex items-center gap-1">{item.icon}{item.label}</div>
            <div className="stat-value text-sm font-medium truncate">{item.value || "-"}</div>
          </div>
        ))}
      </div>

      {/* Tabs */}
      <div className="card bg-base-100 shadow" data-tour="device-tabs">
        <div className="border-b border-base-300">
          <div role="tablist" className="tabs tabs-bordered px-4">
            {tabs.map((tab) => {
              const hasData = device.details?.[tab.detailsKey] !== undefined;
              return (
                <button
                  key={tab.key}
                  role="tab"
                  className={`tab gap-1.5 ${activeTab === tab.key ? "tab-active" : ""}`}
                  onClick={() => setActiveTab(tab.key)}
                >
                  {tab.icon}
                  {tab.label}
                  {hasData && <span className="w-1.5 h-1.5 rounded-full bg-success"></span>}
                </button>
              );
            })}
            <button
              role="tab"
              className={`tab gap-1.5 ${activeTab === "asset" ? "tab-active" : ""}`}
              onClick={() => setActiveTab("asset")}
            >
              <ClipboardList size={16} />
              {t("assets.title")}
            </button>
          </div>
        </div>

        <div className="card-body p-4">
          {activeTab === "asset" ? (
            /* Asset tab — special handling */
            <AssetForm deviceUdid={udid!} />
          ) : (
            <>
              {/* Tab header with sync + action buttons */}
              <div className="flex items-center justify-between mb-3">
                <div>
                  <h3 className="font-semibold flex items-center gap-2">
                    {currentTab?.icon} {currentTab?.label}
                  </h3>
                  {updatedAt && (
                    <p className="text-xs text-base-content/50 mt-0.5">上次同步：{updatedAt}</p>
                  )}
                </div>
                <div className="flex gap-2">
                  {activeTab === "profiles" && (
                    <button onClick={() => setShowInstallProfile(true)} className="btn btn-primary btn-sm gap-1">
                      <Upload size={14} /> 安裝描述檔
                    </button>
                  )}
                  <button
                    onClick={() => currentTab && syncTab(currentTab)}
                    disabled={syncing !== null}
                    className="btn btn-outline btn-sm gap-1"
                  >
                    {syncing === currentTab?.key ? <span className="loading loading-spinner loading-xs"></span> : <RefreshCw size={14} />}
                    同步
                  </button>
                </div>
              </div>

              {/* Tab content */}
              {syncing === currentTab?.key ? (
                <div className="flex items-center justify-center py-8 gap-2 text-base-content/50">
                  <span className="loading loading-spinner loading-md"></span>
                  正在查詢，等待裝置回應...
                </div>
              ) : cachedPayload ? (
                <ResponseViewer
                  rawPayload={cachedPayload}
                  onRemoveProfile={activeTab === "profiles" ? removeProfile : undefined}
                  appIcons={activeTab === "apps" ? Object.fromEntries(managedApps.filter((a) => a.icon_url).map((a) => [a.bundle_id, a.icon_url])) : undefined}
                  managedBundleIds={activeTab === "apps" ? new Set(deviceApps.map((da) => da.bundle_id)) : undefined}
                />
              ) : (
                <div className="text-center py-8 text-base-content/50">
                  尚無資料，點擊「同步」查詢
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Action commands */}
      <div className="card bg-base-100 shadow" data-tour="device-actions">
        <div className="card-body p-4">
          <h2 className="card-title text-base mb-2">裝置操作</h2>
          <div className="flex flex-wrap gap-2">
            {COMMAND_SPECS.map((cmd) => {
              const check = checkCommand(cmd, {
                platform: devicePlatform,
                isSupervised: !!device.is_supervised,
                isLostMode: !!device.is_lost_mode,
                role: userRole,
              });
              // Hide entirely if role doesn't permit (clean toolbar; same behavior as before).
              if (!check.ok && check.reason === "權限不足") return null;
              const disabled = !check.ok || executing !== null;
              return (
                <div key={cmd.method} className={check.reason ? "tooltip" : ""} data-tip={check.reason || ""}>
                  <button
                    onClick={() => executeAction(cmd)}
                    disabled={disabled}
                    className={`btn btn-sm gap-1 ${cmd.danger ? "btn-error" : "btn-outline"}`}
                  >
                    {executing === cmd.method ? <span className="loading loading-spinner loading-xs"></span> : COMMAND_ICONS[cmd.method]}
                    {cmd.label}
                  </button>
                </div>
              );
            })}
          </div>

          {actionResult && (
            <div className="mt-4">
              <h3 className="text-sm font-medium mb-2">{t("commands.result")}</h3>
              <ResponseViewer rawPayload={actionResult} />
            </div>
          )}

          {/* Device location display */}
          {(() => {
            const loc = device.details?.device_location as { latitude?: string; longitude?: string; updated_at?: string } | undefined;
            if (!loc?.latitude || !loc?.longitude) return null;
            const mapUrl = `https://www.google.com/maps?q=${loc.latitude},${loc.longitude}`;
            return (
              <div className="mt-4 p-3 bg-base-200 rounded-lg">
                <h3 className="text-sm font-medium mb-2 flex items-center gap-1"><MapPin size={14} /> 最近定位</h3>
                <div className="text-sm space-y-1">
                  <div>緯度: {loc.latitude}</div>
                  <div>經度: {loc.longitude}</div>
                  {loc.updated_at && <div className="text-xs opacity-50">更新時間: {new Date(loc.updated_at).toLocaleString()}</div>}
                  <a href={mapUrl} target="_blank" rel="noopener noreferrer" className="btn btn-primary btn-xs gap-1 mt-1">
                    <MapPin size={12} /> 在 Google Maps 開啟
                  </a>
                </div>
              </div>
            );
          })()}
        </div>
      </div>
      {/* Install Profile Modal */}
      <dialog className={`modal ${showInstallProfile ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2"><Upload size={18} /> 安裝描述檔</h3>
          <div className="py-4">
            <ProfilePicker
              selectedId={selectedProfileId}
              onSelect={(id, base64) => {
                setSelectedProfileId(id);
                setSelectedProfilePayload(base64);
              }}
            />
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowInstallProfile(false)}>取消</button>
            <button
              className="btn btn-primary"
              disabled={!selectedProfilePayload}
              onClick={installProfile}
            >
              安裝
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowInstallProfile(false)}>close</button>
        </form>
      </dialog>
      {/* Lost Mode Modal */}
      <dialog className={`modal ${showLostMode ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2"><MapPin size={18} /> 啟用遺失模式</h3>
          <div className="space-y-3 py-4">
            <div className="form-control">
              <label className="label"><span className="label-text font-medium">顯示訊息 <span className="text-error">*</span></span></label>
              <input type="text" value={lostModeMessage} onChange={(e) => setLostModeMessage(e.target.value)} className="input input-bordered input-sm" placeholder="此裝置已遺失，請聯繫..." />
            </div>
            <div className="form-control">
              <label className="label"><span className="label-text font-medium">聯絡電話</span></label>
              <input type="text" value={lostModePhone} onChange={(e) => setLostModePhone(e.target.value)} className="input input-bordered input-sm" placeholder="選填" />
            </div>
            <div className="form-control">
              <label className="label"><span className="label-text font-medium">附註</span></label>
              <input type="text" value={lostModeFootnote} onChange={(e) => setLostModeFootnote(e.target.value)} className="input input-bordered input-sm" placeholder="選填" />
            </div>
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowLostMode(false)}>取消</button>
            <button className="btn btn-warning" disabled={!lostModeMessage} onClick={executeLostMode}>啟用</button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowLostMode(false)}>close</button>
        </form>
      </dialog>
      {/* macOS DeviceLock / EraseDevice PIN dialog — Apple requires a 6-digit
          PIN for these on macOS (Apple Silicon needs PIN for erase since 11+,
          and DeviceLock on macOS 10.12+ always requires PIN). */}
      <dialog className={`modal ${pinDialog !== null ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2">
            {pinDialog === "lock" ? <><Lock size={18} /> 鎖定 Mac</> : <><Trash2 size={18} /> 清除 Mac</>}
          </h3>
          <div className="space-y-3 py-4">
            <div className="alert alert-warning text-sm">
              {pinDialog === "lock"
                ? "Mac 收到鎖定後會立即重開機並要求輸入這組 PIN 才能解鎖。請務必記下。"
                : "清除動作不可復原。Mac 重開機進入回復模式需要輸入這組 PIN。"}
            </div>
            <div className="form-control">
              <label className="label"><span className="label-text font-medium">6 位數 PIN <span className="text-error">*</span></span></label>
              <input
                type="text"
                inputMode="numeric"
                pattern="[0-9]{6}"
                maxLength={6}
                value={pinValue}
                onChange={(e) => setPinValue(e.target.value.replace(/\D/g, "").slice(0, 6))}
                className="input input-bordered input-sm font-mono text-center text-lg tracking-widest"
                placeholder="000000"
                autoFocus
              />
              {pinValue.length > 0 && pinValue.length < 6 && (
                <span className="text-error text-xs mt-1">PIN 必須是 6 位數字</span>
              )}
            </div>
            {pinDialog === "lock" && (
              <div className="form-control">
                <label className="label"><span className="label-text font-medium">鎖定畫面訊息</span></label>
                <input
                  type="text"
                  value={lockMessage}
                  onChange={(e) => setLockMessage(e.target.value)}
                  className="input input-bordered input-sm"
                  placeholder="選填，會顯示在鎖定畫面上"
                />
              </div>
            )}
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => { setPinDialog(null); setPinValue(""); setLockMessage(""); }}>取消</button>
            <button
              className={pinDialog === "erase" ? "btn btn-error" : "btn btn-warning"}
              disabled={!/^\d{6}$/.test(pinValue)}
              onClick={pinDialog === "lock" ? executePinLock : executePinErase}
            >
              {pinDialog === "lock" ? "鎖定" : "清除裝置"}
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setPinDialog(null)}>close</button>
        </form>
      </dialog>
      {/* Install App Modal */}
      <dialog className={`modal ${showAppInstall ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2"><Download size={18} /> 安裝 App</h3>
          <div className="py-4">
            {managedApps.length === 0 ? (
              <div className="text-center py-4 text-base-content/50">
                尚未登記任何 App，請先到 App 管理頁面新增
              </div>
            ) : (
              <div className="space-y-2">
                <label className="label"><span className="label-text font-medium">選擇要安裝的 App（可多選）</span></label>
                <div className="border border-base-300 rounded-lg max-h-64 overflow-y-auto divide-y divide-base-200">
                  {managedApps
                    .filter((a) => !deviceApps.some((da) => da.app_id === a.id))
                    .map((a) => {
                      const avail = a.purchased_qty > 0 ? a.purchased_qty - a.installed_count : null;
                      const disabled = avail !== null && avail <= 0;
                      const selected = selectedInstallIds.includes(a.id);
                      return (
                        <div
                          key={a.id}
                          onClick={() => !disabled && setSelectedInstallIds((prev) =>
                            selected ? prev.filter((id) => id !== a.id) : [...prev, a.id]
                          )}
                          className={`flex items-center gap-3 px-3 py-2.5 cursor-pointer transition-colors
                            ${disabled ? "opacity-40 cursor-not-allowed" : "hover:bg-base-200"}
                            ${selected ? "bg-primary/10 border-l-2 border-primary" : ""}`}
                        >
                          <input type="checkbox" checked={selected} disabled={disabled} readOnly className="checkbox checkbox-primary checkbox-sm" />
                          {a.icon_url ? (
                            <img src={a.icon_url} alt="" className="w-9 h-9 rounded-lg flex-shrink-0" />
                          ) : (
                            <div className="w-9 h-9 rounded-lg bg-base-300 flex items-center justify-center flex-shrink-0">
                              <Package size={16} className="opacity-40" />
                            </div>
                          )}
                          <div className="flex-1 min-w-0">
                            <div className="text-sm font-medium">{a.name}</div>
                            <div className="text-xs opacity-50 font-mono truncate">{a.bundle_id}</div>
                          </div>
                          <div className="text-right flex-shrink-0">
                            <span className={`badge badge-xs ${a.app_type === "vpp" ? "badge-primary" : "badge-secondary"}`}>
                              {a.app_type === "vpp" ? "VPP" : "企業"}
                            </span>
                            {avail !== null && (
                              <div className={`text-xs mt-0.5 ${avail <= 0 ? "text-error" : "text-success"}`}>
                                {avail <= 0 ? "已滿" : `可用 ${avail}`}
                              </div>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  {managedApps.filter((a) => !deviceApps.some((da) => da.app_id === a.id)).length === 0 && (
                    <div className="text-center py-4 text-base-content/50 text-sm">所有 App 皆已安裝</div>
                  )}
                </div>
              </div>
            )}
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowAppInstall(false)}>取消</button>
            <button
              className="btn btn-primary gap-1"
              disabled={selectedInstallIds.length === 0 || appLoading}
              onClick={handleInstallApp}
            >
              {appLoading ? <span className="loading loading-spinner loading-xs"></span> : <Download size={14} />}
              安裝 {selectedInstallIds.length > 0 ? `(${selectedInstallIds.length})` : ""}
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowAppInstall(false)}>close</button>
        </form>
      </dialog>
      {/* Update App Modal */}
      <dialog className={`modal ${showAppUpdate ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2"><RefreshCcw size={18} /> 更新 App</h3>
          <p className="text-sm text-base-content/60 mt-1">重新下發安裝指令，裝置會自動更新到最新版本</p>
          <div className="py-4">
            {deviceApps.length === 0 ? (
              <div className="text-center py-4 text-base-content/50">
                此裝置尚未安裝任何受管理的 App
              </div>
            ) : (
              <div className="border border-base-300 rounded-lg max-h-64 overflow-y-auto divide-y divide-base-200">
                {deviceApps.map((da) => {
                  const app = managedApps.find((a) => a.id === da.app_id);
                  const selected = selectedUpdateIds.includes(da.app_id);
                  return (
                    <div
                      key={da.app_id}
                      onClick={() => setSelectedUpdateIds((prev) =>
                        selected ? prev.filter((id) => id !== da.app_id) : [...prev, da.app_id]
                      )}
                      className={`flex items-center gap-3 px-3 py-2.5 cursor-pointer transition-colors hover:bg-base-200
                        ${selected ? "bg-primary/10 border-l-2 border-primary" : ""}`}
                    >
                      <input type="checkbox" checked={selected} readOnly className="checkbox checkbox-primary checkbox-sm" />
                      {app?.icon_url ? (
                        <img src={app.icon_url} alt="" className="w-9 h-9 rounded-lg flex-shrink-0" />
                      ) : (
                        <div className="w-9 h-9 rounded-lg bg-base-300 flex items-center justify-center flex-shrink-0">
                          <Package size={16} className="opacity-40" />
                        </div>
                      )}
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium">{da.app_name}</div>
                        <div className="text-xs opacity-50 font-mono truncate">{da.bundle_id}</div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowAppUpdate(false)}>取消</button>
            <button
              className="btn btn-primary gap-1"
              disabled={selectedUpdateIds.length === 0 || appLoading}
              onClick={handleUpdateApp}
            >
              {appLoading ? <span className="loading loading-spinner loading-xs"></span> : <RefreshCcw size={14} />}
              更新 {selectedUpdateIds.length > 0 ? `(${selectedUpdateIds.length})` : ""}
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowAppUpdate(false)}>close</button>
        </form>
      </dialog>
      {/* Uninstall App Modal */}
      <dialog className={`modal ${showAppUninstall ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg flex items-center gap-2"><PackageMinus size={18} /> 移除 App</h3>
          <p className="text-sm text-base-content/60 mt-1">選擇要從裝置移除的 App（可多選）</p>
          <div className="py-4">
            {deviceApps.length === 0 ? (
              <div className="text-center py-4 text-base-content/50">
                此裝置尚未安裝任何受管理的 App
              </div>
            ) : (
              <div className="border border-base-300 rounded-lg max-h-64 overflow-y-auto divide-y divide-base-200">
                {deviceApps.map((da) => {
                  const app = managedApps.find((a) => a.id === da.app_id);
                  const selected = selectedUninstallIds.includes(da.app_id);
                  return (
                    <div
                      key={da.app_id}
                      onClick={() => setSelectedUninstallIds((prev) =>
                        selected ? prev.filter((id) => id !== da.app_id) : [...prev, da.app_id]
                      )}
                      className={`flex items-center gap-3 px-3 py-2.5 cursor-pointer transition-colors hover:bg-base-200
                        ${selected ? "bg-error/10 border-l-2 border-error" : ""}`}
                    >
                      <input type="checkbox" checked={selected} readOnly className="checkbox checkbox-error checkbox-sm" />
                      {app?.icon_url ? (
                        <img src={app.icon_url} alt="" className="w-9 h-9 rounded-lg flex-shrink-0" />
                      ) : (
                        <div className="w-9 h-9 rounded-lg bg-base-300 flex items-center justify-center flex-shrink-0">
                          <Package size={16} className="opacity-40" />
                        </div>
                      )}
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium">{da.app_name}</div>
                        <div className="text-xs opacity-50 font-mono truncate">{da.bundle_id}</div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowAppUninstall(false)}>取消</button>
            <button
              className="btn btn-error gap-1"
              disabled={selectedUninstallIds.length === 0 || appLoading}
              onClick={handleUninstallApp}
            >
              {appLoading ? <span className="loading loading-spinner loading-xs"></span> : <PackageMinus size={14} />}
              移除 {selectedUninstallIds.length > 0 ? `(${selectedUninstallIds.length})` : ""}
            </button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowAppUninstall(false)}>close</button>
        </form>
      </dialog>
    </div>
  );
}
