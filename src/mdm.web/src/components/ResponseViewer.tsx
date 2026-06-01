import { useMemo, useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useDialog } from "./DialogProvider";
import { parsePlist, base64ToUtf8 } from "../lib/plist";
import { decodeMDMErrorChain, type DecodedError } from "../lib/mdmErrors";
import {
  Smartphone, HardDrive, Wifi, Shield, Lock, Check, X, Trash2,
  Globe, MapPin, Calendar, Package, FileText, Award, AlertTriangle,
} from "lucide-react";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Dict = { [key: string]: any };

interface ResponseViewerProps {
  /** raw_payload — base64 encoded XML plist, or decoded XML, or JSON string */
  rawPayload: string;
  /** The command type that triggered this response (optional, for context) */
  commandType?: string;
  /** Callback when user wants to remove a profile by identifier */
  onRemoveProfile?: (identifier: string) => void;
  /** Map of bundle_id → icon_url for app list display */
  appIcons?: Record<string, string>;
  /** Set of bundle_ids that are managed (installed via MDM) */
  managedBundleIds?: Set<string>;
}

export function ResponseViewer({ rawPayload, commandType, onRemoveProfile, appIcons, managedBundleIds }: ResponseViewerProps) {
  const parsed = useMemo(() => {
    if (!rawPayload) return null;
    // Try as XML directly
    let data = parsePlist(rawPayload);
    if (data) return data as Dict;
    // Try base64 decode (UTF-8 safe) then parse
    try {
      const xml = base64ToUtf8(rawPayload);
      data = parsePlist(xml);
      if (data) return data as Dict;
    } catch { /* not base64 */ }
    // Try JSON
    try {
      return JSON.parse(rawPayload);
    } catch { /* not JSON */ }
    return null;
  }, [rawPayload]);

  if (!parsed) {
    // Fallback: show raw text
    return (
      <pre className="bg-base-200 p-4 rounded-lg overflow-auto text-sm font-mono max-h-96 whitespace-pre-wrap">
        {rawPayload}
      </pre>
    );
  }

  const status = parsed.Status as string;
  const queryResponses = parsed.QueryResponses as Dict | undefined;

  // --- Check QueryResponses (DeviceInformation) ---
  if (queryResponses) {
    if (queryResponses.DeviceName || queryResponses.OSVersion || queryResponses.SerialNumber) {
      return <DeviceInfoView data={queryResponses} status={status} />;
    }
    if (queryResponses.InstalledApplicationList) {
      return <AppListView apps={queryResponses.InstalledApplicationList} status={status} appIcons={appIcons} managedBundleIds={managedBundleIds} />;
    }
    if (queryResponses.ProfileList) {
      return <ProfileListView profiles={queryResponses.ProfileList} status={status} onRemove={onRemoveProfile} />;
    }
    if (queryResponses.SecurityInfo) {
      return <SecurityInfoView data={queryResponses.SecurityInfo} status={status} />;
    }
    if (queryResponses.CertificateList) {
      return <CertListView certs={queryResponses.CertificateList} status={status} />;
    }
    if (queryResponses.AvailableOSUpdates) {
      return <OSUpdatesView updates={queryResponses.AvailableOSUpdates} status={status} />;
    }
    return <GenericDictView data={queryResponses} status={status} />;
  }

  // --- Check TOP-LEVEL keys (InstalledApplicationList, ProfileList, etc.) ---
  if (Array.isArray(parsed.InstalledApplicationList)) {
    return <AppListView apps={parsed.InstalledApplicationList} status={status} appIcons={appIcons} managedBundleIds={managedBundleIds} />;
  }
  if (Array.isArray(parsed.ProfileList)) {
    return <ProfileListView profiles={parsed.ProfileList} status={status} onRemove={onRemoveProfile} />;
  }
  if (parsed.SecurityInfo && typeof parsed.SecurityInfo === "object") {
    return <SecurityInfoView data={parsed.SecurityInfo as Dict} status={status} />;
  }
  if (Array.isArray(parsed.CertificateList)) {
    return <CertListView certs={parsed.CertificateList} status={status} />;
  }
  if (Array.isArray(parsed.AvailableOSUpdates)) {
    return <OSUpdatesView updates={parsed.AvailableOSUpdates} status={status} />;
  }

  // DeviceLocation
  if (parsed.Latitude !== undefined && parsed.Longitude !== undefined) {
    return <LocationView data={parsed} status={status} />;
  }

  // Has multiple keys beyond Status/UDID/CommandUUID → show as generic dict
  const extraKeys = Object.keys(parsed).filter((k) => !["Status", "UDID", "CommandUUID"].includes(k));
  if (extraKeys.length > 0) {
    return <GenericDictView data={parsed} status={status} />;
  }

  // Simple status response
  return <StatusView data={parsed} commandType={commandType} />;
}

// --- Sub Views ---

function StatusBadge({ status }: { status?: string }) {
  if (!status) return null;
  const s = status.toLowerCase();
  const cls = s === "acknowledged" || s === "idle"
    ? "badge-success"
    : s === "error" ? "badge-error" : "badge-info";
  return <span className={`badge ${cls} gap-1`}>
    {s === "acknowledged" || s === "idle" ? <Check size={12} /> : s === "error" ? <X size={12} /> : null}
    {status}
  </span>;
}

function StatusView({ data, commandType }: { data: Dict; commandType?: string }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <StatusBadge status={data.Status} />
        {data.UDID && <span className="font-mono text-xs opacity-60">{data.UDID}</span>}
        {data.CommandUUID && <span className="font-mono text-xs opacity-40">{data.CommandUUID}</span>}
      </div>
      {commandType && (
        <div className="text-sm text-base-content/60">
          {t(`commands.items.${commandType}`, commandType)}
        </div>
      )}
    </div>
  );
}

function DeviceInfoView({ data, status }: { data: Dict; status?: string }) {
  const infoGroups = [
    {
      title: "基本資訊",
      icon: <Smartphone size={16} />,
      items: [
        ["DeviceName", "裝置名稱"],
        ["ModelName", "型號名稱"],
        ["Model", "型號"],
        ["ProductName", "產品名稱"],
        ["SerialNumber", "序號"],
        ["UDID", "UDID"],
      ],
    },
    {
      title: "系統",
      icon: <Package size={16} />,
      items: [
        ["OSVersion", "系統版本"],
        ["BuildVersion", "Build 版本"],
        ["IsSupervised", "受監管"],
        ["IsActivationLockEnabled", "啟用鎖定"],
        ["IsMDMLostModeEnabled", "遺失模式"],
        ["AwaitingConfiguration", "等待設定"],
      ],
    },
    {
      title: "儲存空間",
      icon: <HardDrive size={16} />,
      items: [
        ["DeviceCapacity", "總容量 (GB)"],
        ["AvailableDeviceCapacity", "可用空間 (GB)"],
        ["BatteryLevel", "電量"],
      ],
    },
    {
      title: "網路",
      icon: <Wifi size={16} />,
      items: [
        ["WiFiMAC", "WiFi MAC"],
        ["BluetoothMAC", "藍牙 MAC"],
        ["IMEI", "IMEI"],
        ["PhoneNumber", "電話號碼"],
        ["CurrentCarrierNetwork", "電信業者"],
      ],
    },
  ];

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <StatusBadge status={status} />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {infoGroups.map((group) => {
          const visibleItems = group.items.filter(([key]) => data[key] !== undefined);
          if (visibleItems.length === 0) return null;
          return (
            <div key={group.title} className="border border-base-300 rounded-lg p-3">
              <div className="flex items-center gap-2 font-medium text-sm mb-2 text-base-content/70">
                {group.icon} {group.title}
              </div>
              <div className="space-y-1">
                {visibleItems.map(([key, label]) => (
                  <div key={key} className="flex justify-between text-sm">
                    <span className="text-base-content/60">{label}</span>
                    <span className="font-medium text-right max-w-[60%] truncate">
                      {formatValue(data[key], key)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function AppListView({ apps, status, appIcons, managedBundleIds }: { apps: any[]; status?: string; appIcons?: Record<string, string>; managedBundleIds?: Set<string> }) {
  const [fetchedIcons, setFetchedIcons] = useState<Record<string, string>>({});
  const baseUrl = import.meta.env.DEV ? "" : window.location.origin;

  useEffect(() => {
    const missing = apps
      .map((a) => a.Identifier || a.BundleIdentifier || "")
      .filter((id) => id && !id.startsWith("com.apple.") && !appIcons?.[id] && !fetchedIcons[id]);
    const unique = [...new Set(missing)];
    if (unique.length === 0) return;

    let cancelled = false;
    const batch = unique.slice(0, 30);
    Promise.allSettled(
      batch.map((bundleId) =>
        fetch(`${baseUrl}/api/itunes-lookup?bundleId=${encodeURIComponent(bundleId)}`, { credentials: "include" })
          .then((r) => r.json())
          .then((data) => {
            const result = data?.results?.[0];
            if (result?.artworkUrl60 && !cancelled) {
              return { bundleId, icon: result.artworkUrl60 as string };
            }
            return null;
          })
          .catch(() => null)
      )
    ).then((results) => {
      if (cancelled) return;
      const icons: Record<string, string> = {};
      for (const r of results) {
        if (r.status === "fulfilled" && r.value) {
          icons[r.value.bundleId] = r.value.icon;
        }
      }
      if (Object.keys(icons).length > 0) {
        setFetchedIcons((prev) => ({ ...prev, ...icons }));
      }
    });
    return () => { cancelled = true; };
  }, [apps, appIcons, baseUrl]); // eslint-disable-line react-hooks/exhaustive-deps

  const allIcons = { ...appIcons, ...fetchedIcons };

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <StatusBadge status={status} />
        <span className="text-sm text-base-content/60">{apps.length} apps</span>
      </div>
      <div className="overflow-x-auto max-h-80">
        <table className="table table-xs">
          <thead>
            <tr>
              <th className="w-8"></th>
              <th>名稱</th>
              <th>識別碼</th>
              <th>版本</th>
              <th>大小</th>
              <th>管理</th>
            </tr>
          </thead>
          <tbody>
            {apps.map((app, i) => {
              const bundleId = app.Identifier || app.BundleIdentifier || "";
              const iconUrl = allIcons[bundleId];
              return (
                <tr key={i} className="hover">
                  <td>
                    {iconUrl ? (
                      <img src={iconUrl} alt="" className="w-6 h-6 rounded" />
                    ) : (
                      <div className="w-6 h-6 rounded bg-base-300 flex items-center justify-center">
                        <Package size={12} className="opacity-40" />
                      </div>
                    )}
                  </td>
                  <td className="font-medium">{app.Name || "-"}</td>
                  <td className="font-mono text-xs opacity-70">{bundleId || "-"}</td>
                  <td className="text-xs">{app.ShortVersion || app.Version || "-"}</td>
                  <td className="text-xs">{app.DynamicSize ? formatBytes(app.DynamicSize + (app.StaticSize || 0)) : "-"}</td>
                  <td>{managedBundleIds?.has(bundleId) ? <span className="badge badge-success badge-xs">受管理</span> : bundleId.startsWith("com.apple.") ? <span className="badge badge-info badge-xs">預載</span> : <span className="badge badge-ghost badge-xs">個人</span>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function ProfileListView({ profiles, status, onRemove }: { profiles: any[]; status?: string; onRemove?: (identifier: string) => void }) {
  const dialog = useDialog();
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <FileText size={16} className="opacity-50" />
        <StatusBadge status={status} />
        <span className="text-sm text-base-content/60">{profiles.length} profiles</span>
      </div>
      <div className="overflow-x-auto max-h-96">
        <table className="table table-xs">
          <thead>
            <tr>
              <th>名稱</th>
              <th>識別碼</th>
              <th>組織</th>
              <th>可移除</th>
              {onRemove && <th>操作</th>}
            </tr>
          </thead>
          <tbody>
            {profiles.map((p, i) => (
              <tr key={i} className="hover">
                <td className="font-medium">{p.PayloadDisplayName || p.PayloadDescription || "-"}</td>
                <td className="font-mono text-xs opacity-70">{p.PayloadIdentifier || "-"}</td>
                <td className="text-xs">{p.PayloadOrganization || "-"}</td>
                <td>{p.IsManaged ? <Check size={14} className="text-success" /> : <X size={14} className="text-error" />}</td>
                {onRemove && (
                  <td>
                    <button
                      onClick={async () => {
                        if (await dialog.confirm(`移除描述檔「${p.PayloadDisplayName || p.PayloadIdentifier}」？`)) {
                          onRemove(p.PayloadIdentifier);
                        }
                      }}
                      className="btn btn-ghost btn-xs text-error"
                      title="移除描述檔"
                    >
                      <Trash2 size={14} />
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SecurityInfoView({ data, status }: { data: Dict; status?: string }) {
  const items = [
    ["PasscodePresent", "已設定密碼", <Lock size={14} key="pw" />],
    ["PasscodeCompliant", "密碼合規", <Shield size={14} key="pc" />],
    ["PasscodeCompliantWithProfiles", "符合描述檔要求", <Shield size={14} key="pp" />],
    ["HardwareEncryptionCaps", "硬體加密", <Lock size={14} key="he" />],
    ["IsPasscodeLockGracePeriodEnforced", "密碼寬限期", <Lock size={14} key="gp" />],
    ["FDE_Enabled", "全磁碟加密", <Lock size={14} key="fde" />],
    ["FDE_HasPersonalRecoveryKey", "個人恢復金鑰", <Lock size={14} key="prk" />],
    ["FirewallEnabled", "防火牆", <Shield size={14} key="fw" />],
    ["BlockAllIncoming", "阻擋所有連入", <Shield size={14} key="bi" />],
    ["StealthMode", "隱身模式", <Shield size={14} key="sm" />],
  ] as const;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Shield size={16} className="opacity-50" />
        <StatusBadge status={status} />
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        {items.map(([key, label, icon]) => {
          if (data[key] === undefined) return null;
          const val = data[key];
          return (
            <div key={key} className="flex items-center gap-2 p-2 rounded border border-base-300">
              {icon}
              <span className="text-sm flex-1">{label}</span>
              {typeof val === "boolean" ? (
                val ? <Check size={14} className="text-success" /> : <X size={14} className="text-error" />
              ) : (
                <span className="text-sm font-mono">{String(val)}</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function CertListView({ certs, status }: { certs: any[]; status?: string }) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Award size={16} className="opacity-50" />
        <StatusBadge status={status} />
        <span className="text-sm text-base-content/60">{certs.length} certificates</span>
      </div>
      <div className="overflow-x-auto max-h-80">
        <table className="table table-xs">
          <thead>
            <tr><th>名稱</th><th>是否身份憑證</th></tr>
          </thead>
          <tbody>
            {certs.map((c, i) => (
              <tr key={i} className="hover">
                <td className="font-medium text-sm">{c.CommonName || "-"}</td>
                <td>{c.IsIdentity ? <Check size={14} className="text-success" /> : <X size={14} className="text-base-content/30" />}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function OSUpdatesView({ updates, status }: { updates: any[]; status?: string }) {
  if (updates.length === 0) {
    return (
      <div className="flex items-center gap-2">
        <StatusBadge status={status} />
        <span className="text-sm text-base-content/60">已是最新版本</span>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Package size={16} className="opacity-50" />
        <StatusBadge status={status} />
      </div>
      <div className="space-y-2">
        {updates.map((u, i) => (
          <div key={i} className="border border-base-300 rounded-lg p-3">
            <div className="flex items-center justify-between">
              <div>
                <div className="font-medium">{u.ProductName || "System Update"}</div>
                <div className="text-sm text-base-content/60">
                  版本 {u.ProductVersion || u.Version || "?"} (Build {u.Build || "?"})
                </div>
              </div>
              <div className="text-right">
                {u.DownloadSize && <div className="text-xs text-base-content/50">{formatBytes(u.DownloadSize)}</div>}
                {u.IsCritical && <span className="badge badge-warning badge-xs gap-1"><AlertTriangle size={10} /> 重要</span>}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function LocationView({ data, status }: { data: Dict; status?: string }) {
  // Coerce to number — plist values may arrive as strings; reject anything
  // non-finite so we never paste attacker-controlled text into a URL attribute.
  const lat = typeof data.Latitude === "number" ? data.Latitude : Number(data.Latitude);
  const lng = typeof data.Longitude === "number" ? data.Longitude : Number(data.Longitude);
  const haveCoords = Number.isFinite(lat) && Number.isFinite(lng);
  // encodeURIComponent is defence-in-depth even after the numeric check.
  const mapUrl = haveCoords
    ? `https://www.google.com/maps?q=${encodeURIComponent(String(lat))},${encodeURIComponent(String(lng))}`
    : "";

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <MapPin size={16} className="text-error" />
        <StatusBadge status={status} />
      </div>
      <div className="border border-base-300 rounded-lg p-4">
        <div className="flex items-start gap-4">
          <Globe size={24} className="text-primary mt-1" />
          <div>
            <div className="font-mono text-lg">
              {haveCoords ? `${lat.toFixed(6)}, ${lng.toFixed(6)}` : "—"}
            </div>
            {data.HorizontalAccuracy && (
              <div className="text-sm text-base-content/60">
                精度: {data.HorizontalAccuracy}m
              </div>
            )}
            {data.Timestamp && (
              <div className="text-sm text-base-content/60 flex items-center gap-1">
                <Calendar size={12} /> {new Date(data.Timestamp as string).toLocaleString()}
              </div>
            )}
            {haveCoords && (
              <a href={mapUrl} target="_blank" rel="noopener noreferrer" className="btn btn-primary btn-sm mt-2 gap-1">
                <MapPin size={14} /> Google Maps
              </a>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function GenericDictView({ data, status }: { data: Dict; status?: string }) {
  return (
    <div className="space-y-3">
      <StatusBadge status={status} />
      <div className="overflow-x-auto max-h-80">
        <table className="table table-xs">
          <thead><tr><th>Key</th><th>Value</th></tr></thead>
          <tbody>
            {Object.entries(data).map(([key, val]) => (
              <tr key={key} className="hover">
                <td className="font-mono text-xs font-medium align-top">{key}</td>
                <td className="text-sm"><ValueCell val={val} keyName={key} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ValueCell renders one value in a key/value table. Primitives are shown
// inline (truncated if long); arrays/objects render as a click-to-expand
// disclosure so nested fields like ErrorChain become inspectable.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function ValueCell({ val, keyName }: { val: any; keyName?: string }) {
  const isArray = Array.isArray(val);
  const isObject = !isArray && typeof val === "object" && val !== null && !(val instanceof Date);

  if (!isArray && !isObject) {
    return <span className="max-w-md inline-block truncate align-top">{formatValue(val, keyName)}</span>;
  }

  // Special case: ErrorChain — decode each entry to a human-readable label
  // via the mdmErrors lookup, so operators don't have to cross-reference docs.
  if (isArray && keyName === "ErrorChain") {
    return <ErrorChainCell raw={val as unknown[]} />;
  }

  const summary = isArray
    ? `[${(val as unknown[]).length} items]`
    : `{${Object.keys(val as object).length} keys}`;

  return (
    <details className="group">
      <summary className="cursor-pointer select-none text-primary hover:underline">
        {summary}
      </summary>
      <pre className="mt-1 p-2 text-xs bg-base-200 rounded max-h-64 overflow-auto whitespace-pre-wrap break-words">
        {safeStringify(val)}
      </pre>
    </details>
  );
}

// ErrorChainCell formats an MDM ErrorChain into something operators can act on
// without leaving the page. Each decoded error shows the Chinese label inline
// and folds away the raw fields (ErrorCode/ErrorDomain/LocalizedDescription)
// behind a per-entry disclosure.
function ErrorChainCell({ raw }: { raw: unknown[] }) {
  const decoded: DecodedError[] = decodeMDMErrorChain(raw);
  return (
    <details open>
      <summary className="cursor-pointer select-none text-error hover:underline">
        [{decoded.length} error{decoded.length === 1 ? "" : "s"}]
      </summary>
      <ul className="mt-1 space-y-1">
        {decoded.map((e, i) => (
          <li key={i} className="text-xs border-l-2 border-error/40 pl-2">
            <div className="font-medium">
              {e.label}
              {!e.isKnown && (
                <span className="ml-1 badge badge-xs badge-warning">未收錄</span>
              )}
            </div>
            {e.hint && (
              <div className="text-base-content/70">建議：{e.hint}</div>
            )}
            <div className="font-mono text-base-content/50 mt-0.5">
              {e.domain}{e.code != null ? ` / ${e.code}` : ""}
              {e.localized && e.localized !== e.label && (
                <> · <span className="italic">{e.localized}</span></>
              )}
            </div>
          </li>
        ))}
      </ul>
    </details>
  );
}

// safeStringify handles cyclic refs gracefully (rare in plist data but cheap insurance).
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function safeStringify(val: any): string {
  const seen = new WeakSet();
  try {
    return JSON.stringify(
      val,
      (_k, v) => {
        if (typeof v === "object" && v !== null) {
          if (seen.has(v)) return "[Circular]";
          seen.add(v);
        }
        return v;
      },
      2,
    );
  } catch {
    return String(val);
  }
}

// --- Helpers ---

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function formatValue(val: any, key?: string): string {
  if (val === true) return "Yes";
  if (val === false) return "No";
  if (val instanceof Date) return val.toLocaleString();
  if (typeof val === "number") {
    if (key === "BatteryLevel") return `${Math.round(val * 100)}%`;
    if (key?.includes("Capacity")) return `${val.toFixed(1)} GB`;
    return String(val);
  }
  if (Array.isArray(val)) return `[${val.length} items]`;
  if (typeof val === "object" && val !== null) return JSON.stringify(val);
  return String(val ?? "-");
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
