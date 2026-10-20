import { useCallback, useEffect, useRef, useState } from "react";
import { Calendar, Upload, Link2, Unlink, PlugZap, Trash2, AlertTriangle, CheckCircle2, Copy } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "./DialogProvider";

interface GoogleCalendarStatus {
  encryption_ready: boolean;
  encryption_error?: string;
  client_id: string;
  has_client_secret: boolean;
  redirect_uris: string[];
  callback_path: string;
  connected: boolean;
  connected_account?: string;
  updated_at?: string;
}

interface CalendarInfo {
  id: string;
  summary: string;
  access_role: string;
  primary: boolean;
}

const ROLE_LABELS: Record<string, string> = {
  owner: "擁有者",
  writer: "可編輯",
  reader: "唯讀",
  freeBusyReader: "僅忙碌／空閒",
};

function errorText(err: unknown, fallback: string): string {
  const e = err as { response?: { data?: { error?: string } }; message?: string };
  return e?.response?.data?.error || e?.message || fallback;
}

// Settings for reading business-trip events from a Google Calendar the
// connected account is subscribed to (read-only). The secret and token never
// reach the browser: this page only uploads the credentials file, starts the
// authorisation redirect, and shows status.
export function GoogleCalendarSettings() {
  const dialog = useDialog();
  const [status, setStatus] = useState<GoogleCalendarStatus | null>(null);
  const [loadError, setLoadError] = useState("");
  const [uploading, setUploading] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [calendars, setCalendars] = useState<CalendarInfo[] | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    try {
      const { data } = await apiClient.get<GoogleCalendarStatus>("/api/settings/google-calendar");
      setStatus(data);
      setLoadError("");
    } catch (err) {
      setLoadError(errorText(err, "讀取失敗"));
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    apiClient.get<GoogleCalendarStatus>("/api/settings/google-calendar")
      .then(({ data }) => { if (!cancelled) setStatus(data); })
      .catch((err) => { if (!cancelled) setLoadError(errorText(err, "讀取失敗")); });
    return () => { cancelled = true; };
  }, []);

  // Google sends the admin back to this page with the outcome in the URL.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const outcome = params.get("google_calendar");
    if (!outcome) return;
    const msg = params.get("msg") || "";
    params.delete("google_calendar");
    params.delete("msg");
    const rest = params.toString();
    window.history.replaceState(null, "", window.location.pathname + (rest ? `?${rest}` : ""));
    if (outcome === "connected") dialog.success("Google 帳號已連結，可以按「測試連線」確認讀得到哪些日曆");
    else dialog.error(msg || "Google 授權失敗");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const redirectURI = `${window.location.origin}${status?.callback_path ?? "/api/settings/google-calendar/callback"}`;
  const redirectRegistered = !!status?.redirect_uris.includes(redirectURI);

  const upload = async (file: File) => {
    setUploading(true);
    try {
      const form = new FormData();
      form.append("file", file);
      const { data } = await apiClient.post<GoogleCalendarStatus>("/api/settings/google-calendar/credentials", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      setStatus(data);
      setCalendars(null);
      await dialog.success("憑證已加密儲存。接著請按「連結 Google 帳號」");
    } catch (err) {
      await dialog.error("上傳失敗：" + errorText(err, ""));
    } finally {
      setUploading(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const connect = async () => {
    setConnecting(true);
    try {
      const { data } = await apiClient.post<{ url: string }>("/api/settings/google-calendar/connect", { origin: window.location.origin });
      window.location.href = data.url;
    } catch (err) {
      await dialog.error(errorText(err, "無法開始授權"));
      setConnecting(false);
    }
  };

  const test = async () => {
    setTesting(true);
    setCalendars(null);
    try {
      const { data } = await apiClient.post<{ calendars: CalendarInfo[] }>("/api/settings/google-calendar/test");
      setCalendars(data.calendars);
    } catch (err) {
      await dialog.error(errorText(err, "測試連線失敗"));
    } finally {
      setTesting(false);
    }
  };

  const disconnect = async () => {
    if (!(await dialog.confirm("中斷與 Google 帳號的連結？已上傳的憑證會保留，之後可以重新連結。"))) return;
    try {
      const { data } = await apiClient.post<GoogleCalendarStatus>("/api/settings/google-calendar/disconnect");
      setStatus(data);
      setCalendars(null);
    } catch (err) {
      await dialog.error(errorText(err, "中斷失敗"));
    }
  };

  const remove = async () => {
    if (!(await dialog.confirm("移除 Google 日曆憑證與授權？之後要重新上傳憑證並連結。"))) return;
    try {
      await apiClient.delete("/api/settings/google-calendar");
      setCalendars(null);
      await load();
    } catch (err) {
      await dialog.error(errorText(err, "移除失敗"));
    }
  };

  const copyRedirect = async () => {
    try { await navigator.clipboard.writeText(redirectURI); } catch { /* clipboard may be unavailable (non-HTTPS) */ }
  };

  const badge = !status ? null
    : status.connected ? <span className="badge badge-success badge-sm">已連結</span>
    : status.has_client_secret ? <span className="badge badge-warning badge-sm">待授權</span>
    : <span className="badge badge-ghost badge-sm">尚未設定</span>;

  return (
    <div className="card bg-base-100 shadow">
      <div className="card-body">
        <div className="flex items-center gap-2">
          <Calendar size={18} className="text-primary" />
          <h2 className="card-title text-lg">Google 日曆（讀取出差行程）</h2>
          <div className="ml-auto">{badge}</div>
        </div>
        <p className="text-sm text-base-content/60">
          用一個 Google 帳號授權唯讀存取，讀取它所訂閱的日曆活動，作為出差借車通知的來源。只要求「檢視日曆」權限，不能修改。
        </p>

        {loadError && <div className="alert alert-error text-sm">{loadError}</div>}

        {status && !status.encryption_ready && (
          <div className="alert alert-warning text-sm">
            <AlertTriangle size={16} />
            <div>
              <div className="font-medium">{status.encryption_error}</div>
              <div className="opacity-80">在後端 .env 加入 <code>SETTINGS_ENCRYPTION_KEY</code>（<code>openssl rand -base64 32</code> 產生）並重啟後才能儲存憑證。請另外備份這把金鑰，遺失後已儲存的憑證無法還原。</div>
            </div>
          </div>
        )}

        {status && (
          <div className="space-y-5 mt-1">
            {/* Step 1 */}
            <section className="space-y-2">
              <h3 className="font-medium text-sm">① 上傳 OAuth 用戶端憑證</h3>
              <div className="flex flex-wrap items-center gap-2">
                <input ref={fileRef} type="file" accept=".json,application/json" className="hidden"
                  onChange={(e) => { const f = e.target.files?.[0]; if (f) upload(f); }} />
                <button className="btn btn-sm btn-outline gap-1" disabled={uploading || !status.encryption_ready}
                  onClick={() => fileRef.current?.click()}>
                  {uploading ? <span className="loading loading-spinner loading-xs" /> : <Upload size={14} />}
                  {status.has_client_secret ? "重新上傳 JSON" : "上傳 JSON"}
                </button>
                {status.has_client_secret && (
                  <span className="text-sm text-base-content/70">
                    用戶端 ID：<span className="font-mono text-xs">{status.client_id}</span>（密鑰已加密儲存）
                  </span>
                )}
              </div>
              <p className="text-xs text-base-content/50">
                Google Cloud Console →「API 和服務」→「憑證」→ 建立「OAuth 用戶端 ID」（網頁應用程式）後下載 JSON。重新上傳會清除現有的授權。
              </p>

              <div className="rounded-lg border border-base-300 p-3 text-xs space-y-1">
                <div className="flex items-center gap-2">
                  <span className="opacity-60">此網站的回呼 URI：</span>
                  <span className="font-mono break-all">{redirectURI}</span>
                  <button className="btn btn-ghost btn-xs" onClick={copyRedirect} title="複製"><Copy size={12} /></button>
                </div>
                {status.has_client_secret && (
                  redirectRegistered
                    ? <div className="flex items-center gap-1 text-success"><CheckCircle2 size={12} /> 已在上傳的憑證中註冊</div>
                    : <div className="flex items-center gap-1 text-warning"><AlertTriangle size={12} /> 尚未註冊：請把這個 URI 加到 Google 憑證的「已授權的重新導向 URI」，重新下載 JSON 並上傳</div>
                )}
              </div>
            </section>

            {/* Step 2 */}
            <section className="space-y-2">
              <h3 className="font-medium text-sm">② 連結 Google 帳號</h3>
              <div className="flex flex-wrap items-center gap-2">
                <button className="btn btn-sm btn-primary gap-1"
                  disabled={connecting || !status.has_client_secret || !redirectRegistered || !status.encryption_ready}
                  onClick={connect}>
                  {connecting ? <span className="loading loading-spinner loading-xs" /> : <Link2 size={14} />}
                  {status.connected ? "重新連結" : "連結 Google 帳號"}
                </button>
                {status.connected && (
                  <span className="text-sm text-base-content/70">
                    目前連結：<span className="font-medium">{status.connected_account || "（未知帳號）"}</span>
                  </span>
                )}
              </div>
              <p className="text-xs text-base-content/50">
                會跳轉到 Google 登入並同意「檢視您的日曆」。請用有訂閱出差日曆的那個帳號。
              </p>
            </section>

            {/* Step 3 */}
            <section className="space-y-2">
              <h3 className="font-medium text-sm">③ 測試連線</h3>
              <div className="flex flex-wrap gap-2">
                <button className="btn btn-sm btn-outline gap-1" disabled={testing || !status.connected} onClick={test}>
                  {testing ? <span className="loading loading-spinner loading-xs" /> : <PlugZap size={14} />}
                  列出可讀取的日曆
                </button>
                {status.connected && (
                  <button className="btn btn-sm btn-ghost gap-1" onClick={disconnect}><Unlink size={14} /> 中斷連結</button>
                )}
                {status.has_client_secret && (
                  <button className="btn btn-sm btn-ghost text-error gap-1 ml-auto" onClick={remove}><Trash2 size={14} /> 移除憑證</button>
                )}
              </div>

              {calendars && (
                calendars.length === 0
                  ? <p className="text-sm text-base-content/60">這個帳號沒有任何可讀取的日曆。</p>
                  : (
                    <div className="overflow-x-auto">
                      <table className="table table-sm">
                        <thead><tr><th>名稱</th><th>日曆 ID</th><th>權限</th></tr></thead>
                        <tbody>
                          {calendars.map((c) => (
                            <tr key={c.id}>
                              <td>{c.summary}{c.primary && <span className="badge badge-xs badge-outline ml-1">主要</span>}</td>
                              <td className="font-mono text-xs break-all">{c.id}</td>
                              <td>{ROLE_LABELS[c.access_role] || c.access_role}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )
              )}
            </section>
          </div>
        )}

        {status?.updated_at && status.updated_at !== "0001-01-01T00:00:00Z" && (
          <span className="text-xs text-base-content/50">最後更新：{new Date(status.updated_at).toLocaleString()}</span>
        )}
      </div>
    </div>
  );
}
