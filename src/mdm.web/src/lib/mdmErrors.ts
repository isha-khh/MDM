/**
 * MDM ErrorChain decoder.
 *
 * Apple MDM commands that fail return a plist with an `ErrorChain` array.
 * Each entry has at minimum:
 *   - ErrorDomain          e.g. "MCInstallationErrorDomain"
 *   - ErrorCode            e.g. 4001
 *   - LocalizedDescription (English, sometimes terse)
 *
 * This module maps known (domain, code) pairs to a Chinese label + a
 * remediation hint. Codes we don't recognise fall back to the raw fields.
 *
 * To extend: when you encounter a new error in the UI, add a row in TABLE.
 * Keep the label short (≤ 20 chars) and hint actionable.
 *
 * Sources: Apple official docs + practical experience deploying MicroMDM.
 * Reference: https://developer.apple.com/documentation/devicemanagement/responseerrorcode
 */

interface ErrorMeta {
  label: string; // 中文短描述
  hint?: string; // 處置方向，可空
}

const TABLE: Record<string, Record<number, ErrorMeta>> = {
  // Profile installation — issued by mobileconfig / DEP profile install paths.
  MCInstallationErrorDomain: {
    1001: { label: "內部錯誤", hint: "重試一次，仍失敗就看裝置 console" },
    1009: { label: "Profile 內容無效", hint: "檢查 mobileconfig 結構 / payload" },
    1011: { label: "缺少必要根憑證", hint: "先把 CA 推下去再安裝 profile" },
    4001: { label: "Profile 不存在或不合法", hint: "重新簽署 profile" },
    4002: { label: "Profile 簽章解密失敗", hint: "檢查 CA、簽章鏈是否完整" },
    4005: { label: "Profile 已安裝", hint: "可忽略（重複下命令）" },
    4011: { label: "Profile 被使用者拒絕", hint: "Supervised 模式才能強制安裝" },
  },

  // Top-level MDM protocol errors.
  MCMDMErrorDomain: {
    12011: { label: "命令 payload 格式錯", hint: "檢查送出的 plist 結構" },
    12022: { label: "裝置不支援此命令", hint: "看 OS 版本或裝置類型" },
    12029: { label: "需要 Supervised 模式", hint: "DEP 重新註冊或 Apple Configurator 監督" },
    12041: { label: "裝置睡眠中", hint: "等下次 push 喚醒" },
  },

  // App + book distribution (VPP / managed app).
  MCAppDistributionErrorDomain: {
    9001: { label: "App Store 查詢失敗", hint: "檢查 iTunes store country / bundle id" },
    9002: { label: "找不到該 App", hint: "確認 bundle id / VPP 授權" },
    9003: { label: "VPP license 不足", hint: "加購授權或釋出未用的" },
    9004: { label: "App 已安裝", hint: "可忽略" },
    9009: { label: "需要使用者同意", hint: "User-based VPP；推 Managed Apple ID" },
  },

  // Restrictions enforced by configuration policy.
  RMErrorDomain: {
    1: { label: "Restriction policy 拒絕", hint: "看現行 Restriction profile 設定" },
  },

  // Web Clip specific (less publicly documented; placeholder for additions).
  MCWebClipErrorDomain: {
    25001: { label: "Web Clip URL 不合法", hint: "檢查 URL scheme (http/https) 與長度" },
  },

  // Connection / transport — usually transient.
  MCConnectionErrorDomain: {
    // No specific codes documented; any code here is treated as transient.
  },
};

// Generic "this is a transient connection issue, just retry" domains.
const TRANSIENT_DOMAINS = new Set(["MCConnectionErrorDomain"]);

export interface RawErrorChainEntry {
  ErrorDomain?: string;
  ErrorCode?: number | string; // sometimes arrives as string from plist
  LocalizedDescription?: string;
  USEnglishDescription?: string;
  [key: string]: unknown;
}

export interface DecodedError {
  domain: string;
  code: number | null;
  label: string;        // human-friendly summary
  hint?: string;        // optional remediation
  localized?: string;   // Apple's own localized message, if provided
  isKnown: boolean;     // true if we found a match in TABLE
}

/** Decode one ErrorChain entry to a structured object. */
export function decodeMDMError(err: RawErrorChainEntry): DecodedError {
  const domain = err.ErrorDomain || "(unknown domain)";
  const code = typeof err.ErrorCode === "number"
    ? err.ErrorCode
    : err.ErrorCode != null
      ? Number(err.ErrorCode)
      : null;
  const localized = err.LocalizedDescription || err.USEnglishDescription;

  const meta = code !== null ? TABLE[domain]?.[code] : undefined;
  if (meta) {
    return {
      domain, code,
      label: meta.label,
      hint: meta.hint,
      localized,
      isKnown: true,
    };
  }

  if (TRANSIENT_DOMAINS.has(domain)) {
    return {
      domain, code,
      label: "連線錯誤（暫時性）",
      hint: "等下次 check-in 自動重送",
      localized,
      isKnown: true,
    };
  }

  // Unknown — fall back to whatever Apple sent.
  return {
    domain, code,
    label: localized || "(無說明)",
    localized,
    isKnown: false,
  };
}

/** Decode an entire ErrorChain array. */
export function decodeMDMErrorChain(chain: unknown): DecodedError[] {
  if (!Array.isArray(chain)) return [];
  return chain
    .filter((e): e is RawErrorChainEntry => typeof e === "object" && e !== null)
    .map(decodeMDMError);
}
