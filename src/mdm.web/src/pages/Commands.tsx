import { useState, useEffect } from "react";
import { useSearchParams } from "react-router";
import { useAuthStore } from "../stores/authStore";
import { useEventStore } from "../stores/eventStore";
import { useTranslation } from "react-i18next";
import { useDialog } from "../components/DialogProvider";
import { DevicePicker } from "../components/DevicePicker";
import { ProfilePicker } from "../components/ProfilePicker";
import { AppPicker } from "../components/AppPicker";
import { ResponseViewer } from "../components/ResponseViewer";
import {
  Lock, RotateCcw, Power, KeyRound, Trash2, Download, Building2, PackageMinus,
  FileDown, FileX, Info, AppWindow, FileText, Shield, Award, Package,
  CalendarClock, UserPlus, CheckCircle, Key, MapPin, MapPinOff, Navigation,
  Volume2, Bell, Eraser, Play, RefreshCcw,
} from "lucide-react";
import type { ReactNode } from "react";

interface Command {
  id: string;
  label: string;
  icon: ReactNode;
  method: string;
  fields?: string[];
  danger?: boolean;
  category: string;
  /** If set, shows AppPicker and auto-fills the relevant field */
  appField?: "itunesStoreId" | "manifestUrl" | "identifier";
  appTypeFilter?: "vpp" | "enterprise";
}

const COMMANDS: Command[] = [
  { id: "lock", label: "Lock Device", icon: <Lock size={16} />, method: "lockDevice", fields: ["pin", "message"], category: "Device Control" },
  { id: "restart", label: "Restart", icon: <RotateCcw size={16} />, method: "restartDevice", category: "Device Control" },
  { id: "shutdown", label: "Shutdown", icon: <Power size={16} />, method: "shutdownDevice", category: "Device Control" },
  { id: "clearPasscode", label: "Clear Passcode", icon: <KeyRound size={16} />, method: "clearPasscode", category: "Device Control" },
  { id: "erase", label: "Erase Device", icon: <Trash2 size={16} />, method: "eraseDevice", fields: ["pin"], danger: true, category: "Device Control" },
  { id: "installApp", label: "Install App (VPP)", icon: <Download size={16} />, method: "installApp", fields: ["assignVppLicense"], appField: "itunesStoreId", appTypeFilter: "vpp", category: "App Management" },
  { id: "installEnterprise", label: "Install Enterprise App", icon: <Building2 size={16} />, method: "installEnterpriseApp", appField: "manifestUrl", appTypeFilter: "enterprise", category: "App Management" },
  { id: "updateApp", label: "Update App (VPP)", icon: <RefreshCcw size={16} />, method: "installApp", appField: "itunesStoreId", appTypeFilter: "vpp", category: "App Management" },
  { id: "updateEnterprise", label: "Update Enterprise App", icon: <RefreshCcw size={16} />, method: "installEnterpriseApp", appField: "manifestUrl", appTypeFilter: "enterprise", category: "App Management" },
  { id: "removeApp", label: "Remove App", icon: <PackageMinus size={16} />, method: "removeApp", appField: "identifier", category: "App Management" },
  { id: "installProfile", label: "Install Profile", icon: <FileDown size={16} />, method: "installProfile", fields: ["payload"], category: "Profile" },
  { id: "removeProfile", label: "Remove Profile", icon: <FileX size={16} />, method: "removeProfile", fields: ["identifier"], category: "Profile" },
  { id: "deviceInfo", label: "Device Info", icon: <Info size={16} />, method: "getDeviceInfo", category: "Information" },
  { id: "installedApps", label: "Installed Apps", icon: <AppWindow size={16} />, method: "getInstalledApps", category: "Information" },
  { id: "profileList", label: "Profile List", icon: <FileText size={16} />, method: "getProfileList", category: "Information" },
  { id: "securityInfo", label: "Security Info", icon: <Shield size={16} />, method: "getSecurityInfo", category: "Information" },
  { id: "certList", label: "Certificates", icon: <Award size={16} />, method: "getCertificateList", category: "Information" },
  { id: "osUpdates", label: "OS Updates", icon: <Package size={16} />, method: "getAvailableOSUpdates", category: "Information" },
  { id: "scheduleUpdate", label: "Schedule OS Update", icon: <CalendarClock size={16} />, method: "scheduleOSUpdate", fields: ["productKey", "productVersion", "installAction"], category: "OS Update" },
  { id: "setupAccount", label: "Setup Account", icon: <UserPlus size={16} />, method: "setupAccount", fields: ["fullName", "userName", "lockAccountInfo"], category: "Setup" },
  { id: "deviceConfigured", label: "Mark Configured", icon: <CheckCircle size={16} />, method: "deviceConfigured", category: "Setup" },
  { id: "activationBypass", label: "Activation Bypass", icon: <Key size={16} />, method: "getActivationLockBypass", category: "Setup" },
  { id: "enableLostMode", label: "Enable Lost Mode", icon: <MapPin size={16} />, method: "enableLostMode", fields: ["message", "phoneNumber", "footnote"], category: "Lost Mode" },
  { id: "disableLostMode", label: "Disable Lost Mode", icon: <MapPinOff size={16} />, method: "disableLostMode", category: "Lost Mode" },
  { id: "deviceLocation", label: "Get Location", icon: <Navigation size={16} />, method: "getDeviceLocation", category: "Lost Mode" },
  { id: "playSound", label: "Play Sound", icon: <Volume2 size={16} />, method: "playLostModeSound", category: "Lost Mode" },
  { id: "push", label: "Send Push", icon: <Bell size={16} />, method: "sendPush", category: "Push & Queue" },
  { id: "clearQueue", label: "Clear Queue", icon: <Eraser size={16} />, method: "clearCommandQueue", category: "Push & Queue" },
];

const categories = [...new Set(COMMANDS.map((c) => c.category))];

export function Commands() {
  const { t } = useTranslation();
  const dialog = useDialog();
  const { clients } = useAuthStore();
  // trackCommand is a stable action reference (zustand actions never change
  // identity), so selecting it means this page never re-renders from this
  // store at all — a bare useEventStore() was instead subscribing to every
  // field, including `events`, which is appended to on every incoming MDM
  // event regardless of anything this page reads.
  const trackCommand = useEventStore((s) => s.trackCommand);
  const [searchParams] = useSearchParams();
  const [selectedUdids, setSelectedUdids] = useState<string[]>(() => {
    const param = searchParams.get("udids");
    return param ? param.split(",").filter(Boolean) : [];
  });
  const [selectedCmd, setSelectedCmd] = useState(COMMANDS[0]);
  const [fields, setFields] = useState<Record<string, string | boolean>>({});
  const [result, setResult] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);
  const [selectedProfileId, setSelectedProfileId] = useState("");
  const [selectedAppId, setSelectedAppId] = useState("");

  useEffect(() => {
    setFields({});
    setResult(null);
    setSelectedProfileId("");
    setSelectedAppId("");
  }, [selectedCmd]);

  const handleExecute = async () => {
    if (!clients || selectedUdids.length === 0) {
      await dialog.alert(t("commands.enterUdid"));
      return;
    }
    if (selectedCmd.danger) { setShowConfirm(true); return; }
    await executeCommand();
  };

  const executeCommand = async () => {
    if (!clients) return;
    setShowConfirm(false);
    setLoading(true);
    setResult(null);
    try {
      const reqPayload: Record<string, unknown> = { udids: selectedUdids, ...fields };
      // Convert base64 string payload to raw bytes for InstallProfile
      if (typeof reqPayload.payload === "string" && reqPayload.payload) {
        const binary = atob(reqPayload.payload);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        reqPayload.payload = bytes;
      }
      // @ts-expect-error dynamic method call
      const resp = await clients.command[selectedCmd.method](reqPayload);

      // Track the command for status monitoring
      const cmdLabel = t(`commands.items.${selectedCmd.label}`);
      trackCommand(cmdLabel, selectedUdids, resp.commandUuid);

      setResult(JSON.stringify(resp, null, 2));
    } catch (err) {
      setResult(`Error: ${err instanceof Error ? err.message : "Unknown error"}`);
    } finally { setLoading(false); }
  };

  return (
    <>
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Command list */}
        <div className="card bg-base-100 shadow lg:col-span-1">
          <div className="card-body p-4">
            <h2 className="card-title text-base">{t("commands.title")}</h2>
            <div className="overflow-y-auto max-h-[70vh] space-y-4">
              {categories.map((cat) => (
                <div key={cat}>
                  <div className="text-xs font-semibold uppercase tracking-wider text-base-content/40 mb-1 px-2">
                    {t(`commands.categories.${cat}`)}
                  </div>
                  <ul className="menu menu-sm p-0 gap-0.5">
                    {COMMANDS.filter((c) => c.category === cat).map((cmd) => (
                      <li key={cmd.id}>
                        <button
                          onClick={() => setSelectedCmd(cmd)}
                          className={`${selectedCmd.id === cmd.id ? "active" : ""} ${cmd.danger ? "text-error" : ""}`}
                        >
                          {cmd.icon}
                          {t(`commands.items.${cmd.label}`)}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Command form */}
        <div className="lg:col-span-2 space-y-4">
          <div className="card bg-base-100 shadow">
            <div className="card-body">
              <h2 className="card-title gap-2">
                {selectedCmd.icon}
                {t(`commands.items.${selectedCmd.label}`)}
                {selectedCmd.danger && <span className="badge badge-error badge-sm">{t("commands.danger")}</span>}
              </h2>

              <div className="space-y-4 mt-2">
                <div className="form-control">
                  <label className="label">
                    <span className="label-text font-medium">{t("commands.targetDevices")}</span>
                  </label>
                  <DevicePicker selected={selectedUdids} onChange={setSelectedUdids} />
                </div>

                {/* App picker for app-related commands */}
                {selectedCmd.appField && (
                  <div className="form-control">
                    <label className="label">
                      <span className="label-text font-medium">選擇 App</span>
                    </label>
                    <AppPicker
                      appType={selectedCmd.appTypeFilter}
                      selectedId={selectedAppId}
                      onSelect={(app) => {
                        if (app) {
                          setSelectedAppId(app.id);
                          if (selectedCmd.appField === "itunesStoreId") {
                            setFields((f) => ({ ...f, itunesStoreId: app.itunes_store_id }));
                          } else if (selectedCmd.appField === "manifestUrl") {
                            setFields((f) => ({ ...f, manifestUrl: app.manifest_url }));
                          } else if (selectedCmd.appField === "identifier") {
                            setFields((f) => ({ ...f, identifier: app.bundle_id }));
                          }
                        } else {
                          setSelectedAppId("");
                        }
                      }}
                    />
                  </div>
                )}

                {/* Other fields */}
                {selectedCmd.fields?.map((field) => (
                  <div key={field} className="form-control">
                    <label className="label">
                      <span className="label-text font-medium">{t(`commands.fields.${field}`)}</span>
                    </label>
                    {field === "payload" ? (
                      <ProfilePicker
                        selectedId={selectedProfileId}
                        onSelect={(id, base64) => {
                          setSelectedProfileId(id);
                          setFields({ ...fields, payload: base64 });
                        }}
                      />
                    ) : field.includes("lock") || field === "assignVppLicense" ? (
                      <input
                        type="checkbox"
                        checked={(fields[field] as boolean) || false}
                        onChange={(e) => setFields({ ...fields, [field]: e.target.checked })}
                        className="toggle toggle-primary"
                      />
                    ) : (
                      <input
                        type="text"
                        value={(fields[field] as string) || ""}
                        onChange={(e) => setFields({ ...fields, [field]: e.target.value })}
                        className="input input-bordered"
                      />
                    )}
                  </div>
                ))}

                <button
                  onClick={handleExecute}
                  disabled={loading}
                  className={`btn gap-2 ${selectedCmd.danger ? "btn-error" : "btn-primary"}`}
                >
                  {loading ? <span className="loading loading-spinner loading-sm"></span> : <Play size={16} />}
                  {loading ? t("commands.executing") : t("commands.execute")}
                </button>
              </div>
            </div>
          </div>

          {result && (
            <div className="card bg-base-100 shadow">
              <div className="card-body">
                <h3 className="card-title text-base">{t("commands.result")}</h3>
                <ResponseViewer rawPayload={result} commandType={selectedCmd.label} />
              </div>
            </div>
          )}
        </div>
      </div>

      <dialog className={`modal ${showConfirm ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className="font-bold text-lg text-error">{t("commands.dangerConfirm")}</h3>
          <p className="py-4">{t("commands.dangerMessage", { action: t(`commands.items.${selectedCmd.label}`) })}</p>
          <div className="modal-action">
            <button className="btn" onClick={() => setShowConfirm(false)}>{t("common.cancel")}</button>
            <button className="btn btn-error" onClick={executeCommand}>{t("common.confirm")}</button>
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => setShowConfirm(false)}>close</button>
        </form>
      </dialog>
    </>
  );
}
