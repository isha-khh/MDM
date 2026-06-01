/**
 * MDM Command capability matrix.
 *
 * Each Apple MDM command has different platform support, supervised-mode
 * requirements, required parameters, and pre-conditions. This module
 * centralises that knowledge so the UI can:
 *   - disable buttons that don't apply to the current device
 *   - show a tooltip explaining why
 *   - open the right parameter dialog before sending
 *
 * Reference: https://developer.apple.com/documentation/devicemanagement
 * + practical experience deploying MicroMDM.
 */

export type Platform = "ios" | "ipados" | "macos" | "tvos" | "watchos" | "unknown";

/** Which dialog component to open before executing (key matches DeviceDetail state). */
export type DialogKey =
  | "lock-pin"     // 6-digit PIN required for macOS DeviceLock
  | "erase-pin"    // 6-digit PIN required for macOS EraseDevice
  | "lost-mode"    // EnableLostMode: Message + PhoneNumber + Footnote
  | "install-app"
  | "uninstall-app"
  | "update-app";

export interface MDMCommandSpec {
  /** Method on `clients.command` (or a sentinel like `"showInstallApp"`). */
  method: string;
  /** 中文按鈕標籤 */
  label: string;
  /** Platforms where the underlying Apple command is supported. */
  platforms: ReadonlyArray<Platform>;
  /** True if Apple requires the device to be in Supervised mode. */
  requiresSupervised: boolean;
  /** True if Apple requires the device to currently be in Lost Mode. */
  requiresLostMode: boolean;
  /** Which parameter dialog to open before sending. */
  dialog?: DialogKey;
  /** Dialog is required on these specific platforms (e.g. PIN only on macOS). Empty = always required. */
  dialogRequiredOn?: ReadonlyArray<Platform>;
  /** Visual marker (red button). */
  danger?: boolean;
  /** Allowed roles; empty/undefined = all roles. */
  roles?: ReadonlyArray<string>;
}

// All commands the UI currently surfaces. Keep this list in sync with
// `docs/command-capability-matrix.md`.
export const COMMAND_SPECS: ReadonlyArray<MDMCommandSpec> = [
  {
    method: "sendPush",
    label: "推播",
    platforms: ["ios", "ipados", "macos", "tvos", "watchos"],
    requiresSupervised: false,
    requiresLostMode: false,
  },
  {
    method: "lockDevice",
    label: "鎖定裝置",
    platforms: ["ios", "ipados", "macos"],
    requiresSupervised: false,
    requiresLostMode: false,
    dialog: "lock-pin",
    dialogRequiredOn: ["macos"], // iOS: no PIN; macOS: 6-digit PIN REQUIRED
  },
  {
    method: "restartDevice",
    label: "重新啟動",
    platforms: ["ios", "ipados", "macos", "tvos"],
    requiresSupervised: true,
    requiresLostMode: false,
  },
  {
    method: "shutdownDevice",
    label: "關機",
    platforms: ["ios", "ipados", "macos"],
    requiresSupervised: true,
    requiresLostMode: false,
  },
  {
    method: "clearPasscode",
    label: "清除密碼",
    platforms: ["ios", "ipados"],
    requiresSupervised: false,
    requiresLostMode: false,
  },
  {
    method: "enableLostMode",
    label: "啟用遺失模式",
    platforms: ["ios", "ipados"],
    requiresSupervised: true,
    requiresLostMode: false,
    dialog: "lost-mode",
  },
  {
    method: "disableLostMode",
    label: "關閉遺失模式",
    platforms: ["ios", "ipados"],
    requiresSupervised: true,
    requiresLostMode: false, // can disable from any state
  },
  {
    method: "getDeviceLocation",
    label: "定位",
    platforms: ["ios", "ipados"],
    requiresSupervised: true,
    requiresLostMode: true,
  },
  {
    method: "playLostModeSound",
    label: "播放聲音",
    platforms: ["ios", "ipados"],
    requiresSupervised: true,
    requiresLostMode: true,
  },
  {
    method: "showInstallApp",
    label: "安裝 App",
    platforms: ["ios", "ipados", "macos", "tvos"],
    requiresSupervised: false,
    requiresLostMode: false,
    dialog: "install-app",
    roles: ["admin", "operator"],
  },
  {
    method: "showUpdateApp",
    label: "更新 App",
    platforms: ["ios", "ipados", "macos", "tvos"],
    requiresSupervised: false,
    requiresLostMode: false,
    dialog: "update-app",
  },
  {
    method: "showUninstallApp",
    label: "移除 App",
    platforms: ["ios", "ipados", "macos"],
    requiresSupervised: false, // iOS technically needs supervised for unmanaged apps; managed apps don't
    requiresLostMode: false,
    dialog: "uninstall-app",
    roles: ["admin", "operator"],
  },
  {
    method: "eraseDevice",
    label: "清除裝置",
    platforms: ["ios", "ipados", "macos", "tvos"],
    requiresSupervised: false, // iOS managed can erase if MDM is allowed; supervised relax this
    requiresLostMode: false,
    dialog: "erase-pin",
    dialogRequiredOn: ["macos"], // macOS 11+ requires 6-digit PIN
    danger: true,
    roles: ["admin"],
  },
];

/**
 * Infer a device's platform from its model string. MicroMDM doesn't always
 * give us a clean platform field; the model name is the most reliable hint.
 */
export function inferPlatform(model: string, productName?: string): Platform {
  const m = (model + " " + (productName || "")).toLowerCase();
  if (m.includes("ipad")) return "ipados";
  if (m.includes("iphone") || m.includes("ipod")) return "ios";
  if (m.includes("appletv") || m.includes("apple tv")) return "tvos";
  if (m.includes("watch")) return "watchos";
  if (m.includes("mac")) return "macos"; // MacBook, MacBookPro, iMac, Mac mini, Mac Studio, Mac Pro
  return "unknown";
}

export interface CompatCheck {
  ok: boolean;
  reason?: string; // why disabled, suitable for tooltip
}

/**
 * Check whether a command can be executed on a device given its state.
 * Returns `ok: true` if everything passes, otherwise a Chinese reason.
 */
export function checkCommand(
  spec: MDMCommandSpec,
  ctx: {
    platform: Platform;
    isSupervised: boolean;
    isLostMode: boolean;
    role: string;
  },
): CompatCheck {
  // Role gate (existing behaviour)
  if (spec.roles && spec.roles.length > 0 && !spec.roles.includes(ctx.role)) {
    return { ok: false, reason: "權限不足" };
  }
  // Platform gate. Unknown platform passes — we don't want to be over-restrictive
  // when the device's model isn't fingerprinted yet.
  if (ctx.platform !== "unknown" && !spec.platforms.includes(ctx.platform)) {
    return { ok: false, reason: `此命令不支援 ${platformLabel(ctx.platform)}` };
  }
  // Supervised gate
  if (spec.requiresSupervised && !ctx.isSupervised) {
    return { ok: false, reason: "需 Supervised（DEP 監督）裝置" };
  }
  // Lost Mode gate (preserves old behaviour)
  if (spec.requiresLostMode && !ctx.isLostMode) {
    return { ok: false, reason: "需先啟用遺失模式" };
  }
  return { ok: true };
}

function platformLabel(p: Platform): string {
  switch (p) {
    case "ios": return "iPhone / iPod";
    case "ipados": return "iPad";
    case "macos": return "Mac";
    case "tvos": return "Apple TV";
    case "watchos": return "Apple Watch";
    default: return p;
  }
}

/** Whether the dialog is needed before sending on the current platform. */
export function dialogRequired(spec: MDMCommandSpec, platform: Platform): boolean {
  if (!spec.dialog) return false;
  if (!spec.dialogRequiredOn || spec.dialogRequiredOn.length === 0) return true;
  return spec.dialogRequiredOn.includes(platform);
}
