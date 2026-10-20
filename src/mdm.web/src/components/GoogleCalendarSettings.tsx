import { useCallback, useEffect, useRef, useState } from "react";
import { Calendar, Upload, Link2, Unlink, PlugZap, Trash2, AlertTriangle, CheckCircle2, Copy, Eye, Save } from "lucide-react";
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
  selected_calendar_ids: string[];
  updated_at?: string;
}

interface PreviewEvent {
  id: string;
  summary: string;
  description: string;
  description_truncated?: boolean;
  location: string;
  start: string;
  end: string;
  all_day: boolean;
  html_link: string;
  calendar_id: string;
  calendar_summary: string;
  trip: { location: string; time: string; transport: string; names: string[]; emails: string[] };
  location_used: string;
  location_source: "description" | "event" | "title" | "";
  target_location: boolean;
  candidates: Candidate[];
  notice_due: boolean;
  notice_from?: string;
  days_until: number;
}

type Decision = "notify" | "missing_data" | "no_license" | "unmatched";

interface Candidate {
  name?: string;
  email?: string;
  account?: string;
  display_name?: string;
  decision: Decision;
  has_driver_license: boolean | null;
  has_own_car: boolean | null;
  priority: number;
}

const DECISION_BADGE: Record<Decision, { cls: string; label: string }> = {
  notify: { cls: "badge-success", label: "會通知" },
  missing_data: { cls: "badge-warning", label: "資料不全" },
  no_license: { cls: "badge-ghost", label: "無駕照" },
  unmatched: { cls: "badge-error", label: "找不到人" },
};

const LOCATION_SOURCE_LABELS: Record<string, string> = {
  description: "說明欄「地點：」",
  event: "活動的地點欄位",
  title: "標題括號內",
};

function triLabel(v: boolean | null | undefined, yes: string, no: string): string {
  return v === true ? yes : v === false ? no : "未填";
}

interface Preview {
  days: number;
  targets: string[];
  calendars: { id: string; summary: string; count: number; error?: string }[];
  directory?: { configured: boolean; users?: number; error?: string };
  events: PreviewEvent[];
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

// An older backend (not yet restarted after an update) answers without the
// newer fields; fill them in so the page renders and can say what's wrong
// rather than crashing.
const BACKEND_OUTDATED_NOTE = "後端還是舊版本（缺少授權流程的功能），請重啟後端後重新整理頁面";
function normalize(data: GoogleCalendarStatus): GoogleCalendarStatus {
  return { ...data, redirect_uris: data.redirect_uris ?? [], selected_calendar_ids: data.selected_calendar_ids ?? [] };
}

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
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [savingPick, setSavingPick] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [onlyTargets, setOnlyTargets] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    try {
      const { data } = await apiClient.get<GoogleCalendarStatus>("/api/settings/google-calendar");
      setStatus(normalize(data));
      setLoadError("");
    } catch (err) {
      setLoadError(errorText(err, "讀取失敗"));
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    apiClient.get<GoogleCalendarStatus>("/api/settings/google-calendar")
      .then(({ data }) => { if (!cancelled) setStatus(normalize(data)); })
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
      setStatus(normalize(data));
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
      setPicked(new Set(status?.selected_calendar_ids ?? []));
    } catch (err) {
      await dialog.error(errorText(err, "測試連線失敗"));
    } finally {
      setTesting(false);
    }
  };

  const togglePick = (id: string) =>
    setPicked((prev) => { const n = new Set(prev); if (n.has(id)) n.delete(id); else n.add(id); return n; });

  const savePick = async () => {
    setSavingPick(true);
    try {
      const { data } = await apiClient.put<GoogleCalendarStatus>("/api/settings/google-calendar/calendars", { calendar_ids: [...picked] });
      setStatus(normalize(data));
      setPreview(null);
    } catch (err) {
      await dialog.error("儲存失敗：" + errorText(err, ""));
    } finally {
      setSavingPick(false);
    }
  };

  const loadPreview = async () => {
    setPreviewing(true);
    try {
      const { data } = await apiClient.get<Preview>("/api/settings/google-calendar/events");
      setPreview(data);
    } catch (err) {
      await dialog.error(errorText(err, "讀取活動失敗"));
    } finally {
      setPreviewing(false);
    }
  };

  const disconnect = async () => {
    if (!(await dialog.confirm("中斷與 Google 帳號的連結？已上傳的憑證會保留，之後可以重新連結。"))) return;
    try {
      const { data } = await apiClient.post<GoogleCalendarStatus>("/api/settings/google-calendar/disconnect");
      setStatus(normalize(data));
      setCalendars(null);
      setPreview(null);
    } catch (err) {
      await dialog.error(errorText(err, "中斷失敗"));
    }
  };

  const remove = async () => {
    if (!(await dialog.confirm("移除 Google 日曆憑證與授權？之後要重新上傳憑證並連結。"))) return;
    try {
      await apiClient.delete("/api/settings/google-calendar");
      setCalendars(null);
      setPreview(null);
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

        {status && !status.callback_path && (
          <div className="alert alert-warning text-sm">
            <AlertTriangle size={16} />
            <span>{BACKEND_OUTDATED_NOTE}</span>
          </div>
        )}

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
              <h3 className="font-medium text-sm">③ 選擇要讀取的日曆</h3>
              <div className="flex flex-wrap gap-2">
                <button className="btn btn-sm btn-outline gap-1" disabled={testing || !status.connected} onClick={test}>
                  {testing ? <span className="loading loading-spinner loading-xs" /> : <PlugZap size={14} />}
                  {calendars ? "重新列出日曆" : "列出可讀取的日曆"}
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
                    <div className="space-y-2">
                      <p className="text-xs text-base-content/50">勾選要讀取出差行程的日曆（可複選），再按「儲存選擇」。</p>
                      <div className="overflow-x-auto">
                        <table className="table table-sm">
                          <thead><tr><th></th><th>名稱</th><th>日曆 ID</th><th>權限</th></tr></thead>
                          <tbody>
                            {calendars.map((c) => (
                              <tr key={c.id} className="hover cursor-pointer" onClick={() => togglePick(c.id)}>
                                <td><input type="checkbox" className="checkbox checkbox-sm" checked={picked.has(c.id)} onChange={() => togglePick(c.id)} onClick={(e) => e.stopPropagation()} /></td>
                                <td>{c.summary}{c.primary && <span className="badge badge-xs badge-outline ml-1">主要</span>}</td>
                                <td className="font-mono text-xs break-all">{c.id}</td>
                                <td>{ROLE_LABELS[c.access_role] || c.access_role}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                      <button className="btn btn-sm btn-primary gap-1" disabled={savingPick} onClick={savePick}>
                        {savingPick ? <span className="loading loading-spinner loading-xs" /> : <Save size={14} />}
                        儲存選擇（{picked.size} 個）
                      </button>
                    </div>
                  )
              )}
            </section>

            {/* Step 4 */}
            <section className="space-y-2">
              <h3 className="font-medium text-sm">④ 預覽活動與解析結果</h3>
              <p className="text-xs text-base-content/50">
                讀取已選日曆未來 60 天的活動，顯示說明欄解析出的地點／人員／信箱，以及地點是否屬於需要借車的縣市
                {preview ? `（${preview.targets.join("、")}）` : "（台南、高雄、屏東）"}。只讀取，不會寄信也不會寫入任何資料。
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <button className="btn btn-sm btn-outline gap-1" disabled={previewing || !status.connected || status.selected_calendar_ids.length === 0} onClick={loadPreview}>
                  {previewing ? <span className="loading loading-spinner loading-xs" /> : <Eye size={14} />}
                  預覽活動
                </button>
                {status.connected && status.selected_calendar_ids.length === 0 && (
                  <span className="text-xs text-base-content/50">先在上一步選擇並儲存要讀取的日曆</span>
                )}
                {preview && (
                  <label className="label cursor-pointer gap-2 p-0">
                    <input type="checkbox" className="checkbox checkbox-xs" checked={onlyTargets} onChange={(e) => setOnlyTargets(e.target.checked)} />
                    <span className="label-text text-xs">只看符合地點的</span>
                  </label>
                )}
              </div>

              {preview && (
                <div className="space-y-2">
                  {preview.calendars.map((c) => (
                    <div key={c.id} className="text-xs">
                      <span className="font-medium">{c.summary}</span>：
                      {c.error ? <span className="text-error">{c.error}</span> : <span className="opacity-70">{c.count} 筆活動</span>}
                    </div>
                  ))}
                  {(() => {
                    const rows = preview.events.filter((ev) => !onlyTargets || ev.target_location);
                    if (rows.length === 0) return <p className="text-sm text-base-content/60">沒有符合的活動。</p>;
                    return (
                      <div className="space-y-2">
                        {rows.map((ev) => (
                          <details key={ev.calendar_id + ev.id} className="rounded-lg border border-base-300">
                            <summary className="cursor-pointer p-2 text-sm flex flex-wrap items-center gap-2">
                              <span className="font-mono text-xs opacity-70">{ev.all_day ? ev.start : ev.start.replace("T", " ").slice(0, 16)}</span>
                              <span className="font-medium">{ev.summary || "（無標題）"}</span>
                              {ev.target_location
                                ? <span className="badge badge-success badge-sm">符合地點</span>
                                : <span className="badge badge-ghost badge-sm">{ev.location_used ? "不符合" : "沒有地點"}</span>}
                              {ev.target_location && ev.candidates.length > 0 && (
                                ev.notice_due
                                  ? <span className="badge badge-info badge-sm">今天起可通知</span>
                                  : ev.days_until > 0
                                    ? <span className="badge badge-ghost badge-sm">{ev.notice_from} 起通知</span>
                                    : <span className="badge badge-ghost badge-sm">已出發</span>
                              )}
                              {ev.trip.emails.length + ev.trip.names.length > 0 && (
                                <span className="text-xs opacity-60">{ev.trip.names.length} 人／{ev.trip.emails.length} 信箱</span>
                              )}
                              <span className="text-xs opacity-50 ml-auto">{ev.calendar_summary}</span>
                            </summary>
                            <div className="p-3 border-t border-base-300 text-xs space-y-2">
                              <dl className="grid grid-cols-[5rem_1fr] gap-x-2 gap-y-1">
                                <dt className="opacity-60">判斷地點</dt>
                                <dd>{ev.location_used || "—"}{ev.location_source && <span className="opacity-50">（來自{LOCATION_SOURCE_LABELS[ev.location_source] ?? ev.location_source}）</span>}</dd>
                                <dt className="opacity-60">時間</dt><dd>{ev.trip.time || "—"}</dd>
                                <dt className="opacity-60">交通</dt><dd>{ev.trip.transport || "—"}</dd>
                                <dt className="opacity-60">協會人員</dt><dd>{ev.trip.names.join("、") || "—"}</dd>
                                <dt className="opacity-60">信箱</dt><dd className="break-all">{ev.trip.emails.join("、") || "—"}</dd>
                              </dl>
                              {ev.target_location && (
                                <div>
                                  <div className="opacity-60 mb-1">
                                    借車通知對象（沒有自己車的排前面）
                                    {ev.candidates.length > 0 && ev.days_until > 0 && <span>：出發前 7 天（{ev.notice_from}）起寄出，還有 {ev.days_until} 天出發</span>}
                                  </div>
                                  {!preview.directory?.configured
                                    ? <p className="opacity-60">尚未設定人員目錄，無法比對駕照與車輛。</p>
                                    : preview.directory.error
                                      ? <p className="text-error">人員目錄讀取失敗：{preview.directory.error}</p>
                                      : ev.candidates.length === 0
                                        ? <p className="opacity-60">說明欄沒有可辨識的人員。</p>
                                        : (
                                          <table className="table table-xs">
                                            <thead><tr><th>#</th><th>人員</th><th>判定</th><th>駕照</th><th>自有車</th></tr></thead>
                                            <tbody>
                                              {ev.candidates.map((c, i) => (
                                                <tr key={(c.account || c.email || c.name || "") + i}>
                                                  <td>{i + 1}</td>
                                                  <td>
                                                    <div>{c.name || c.display_name || c.email}</div>
                                                    {c.email && <div className="opacity-50 break-all">{c.email}</div>}
                                                  </td>
                                                  <td><span className={`badge badge-sm ${DECISION_BADGE[c.decision].cls}`}>{DECISION_BADGE[c.decision].label}</span></td>
                                                  <td>{c.decision === "unmatched" ? "—" : triLabel(c.has_driver_license, "有", "無")}</td>
                                                  <td>{c.decision === "unmatched" ? "—" : triLabel(c.has_own_car, "有", "無")}</td>
                                                </tr>
                                              ))}
                                            </tbody>
                                          </table>
                                        )}
                                </div>
                              )}
                              <div>
                                <div className="opacity-60 mb-1">說明欄原文{ev.description_truncated && "（已截斷）"}</div>
                                <pre className="whitespace-pre-wrap break-words bg-base-200 rounded p-2 max-h-48 overflow-auto">{ev.description || "（空白）"}</pre>
                              </div>
                              {ev.html_link && <a className="link link-primary" href={ev.html_link} target="_blank" rel="noreferrer">在 Google 日曆開啟</a>}
                            </div>
                          </details>
                        ))}
                      </div>
                    );
                  })()}
                </div>
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
