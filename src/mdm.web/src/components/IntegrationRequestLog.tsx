import { Fragment, useCallback, useEffect, useState } from "react";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import apiClient from "../lib/apiClient";

interface RequestSummary {
  id: number;
  time: string;
  method: string;
  path: string;
  query: string;
  status: number;
  duration_ms: number;
  client: string;
  ip: string;
  idempotency_key: string;
  error_code: string;
  error_message: string;
  key_hint: string;
  replayed: boolean;
}

interface RequestDetail extends RequestSummary {
  request_body?: string;
  response_body?: string;
  request_truncated?: boolean;
  response_truncated?: boolean;
}

const HTTP_TEXT: Record<number, string> = {
  200: "OK", 201: "Created", 400: "Bad Request", 401: "Unauthorized", 403: "Forbidden", 404: "Not Found",
  405: "Method Not Allowed", 409: "Conflict", 422: "Unprocessable Entity", 500: "Internal Server Error",
  503: "Service Unavailable",
};

const statusBadge = (s: number) => (s >= 500 ? "badge-error" : s >= 400 ? "badge-warning" : "badge-success");

const clock = (iso: string) => {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};

/** Bodies are JSON; show them indented when they parse (a body cut at the size cap will not). */
function pretty(text?: string): string {
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-3 py-0.5 text-sm">
      <dt className="w-24 shrink-0 text-base-content/50">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

function Body({ text, truncated }: { text?: string; truncated?: boolean }) {
  if (!text) return <p className="text-sm text-base-content/40">（沒有內容）</p>;
  return (
    <>
      <pre className="bg-base-200 rounded p-2 text-xs font-mono overflow-auto max-h-72 whitespace-pre-wrap break-all">{pretty(text)}</pre>
      {truncated && <p className="text-xs text-warning mt-1">內容過長，只保留前 8 KB。</p>}
    </>
  );
}

/** One call: what came in on the left, what went out (with the HTTP status and error code) on the right. */
function RequestCard({ r }: { r: RequestDetail }) {
  const failed = r.status >= 400;
  return (
    <div className="rounded-lg border border-base-300 bg-base-100 p-4 grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-4">
      <section className="min-w-0">
        <h4 className="font-semibold mb-1">傳入參數</h4>
        <dl>
          <Row label="請求"><span className="font-mono text-xs">{r.method} {r.path}{r.query ? `?${r.query}` : ""}</span></Row>
          <Row label="呼叫端">{r.client || <span className="opacity-50">未通過驗證</span>}</Row>
          <Row label="來源 IP"><span className="font-mono text-xs">{r.ip || "-"}</span></Row>
          {r.key_hint && <Row label="使用的 key"><span className="font-mono text-xs">{r.key_hint}…</span><span className="opacity-50 ml-2">（無法辨識；只記開頭）</span></Row>}
          {r.idempotency_key && <Row label="Idempotency-Key"><span className="font-mono text-xs">{r.idempotency_key}</span></Row>}
        </dl>
        <p className="text-xs text-base-content/50 mt-2 mb-1">請求內容</p>
        <Body text={r.request_body} truncated={r.request_truncated} />
      </section>
      <section className="min-w-0">
        <h4 className="font-semibold mb-1">傳出參數</h4>
        <dl>
          <Row label="HTTP 狀態">
            <span className={`badge badge-sm ${statusBadge(r.status)}`}>{r.status} {HTTP_TEXT[r.status] || ""}</span>
            {r.replayed && <span className="badge badge-sm badge-info badge-outline ml-2" title="同一個 Idempotency-Key 的重送，回傳第一次的結果">重送回放</span>}
          </Row>
          {failed && (
            <>
              <Row label="錯誤代碼"><span className="font-mono text-xs text-error">{r.error_code || "-"}</span></Row>
              <Row label="錯誤訊息">{r.error_message || "-"}</Row>
            </>
          )}
          <Row label="耗時">{r.duration_ms} ms</Row>
        </dl>
        <p className="text-xs text-base-content/50 mt-2 mb-1">回應內容</p>
        <Body text={r.response_body} truncated={r.response_truncated} />
      </section>
    </div>
  );
}

/** The most recent calls to the integration API (kept in the server's memory; gone after a restart). */
export function IntegrationRequestLog() {
  const [rows, setRows] = useState<RequestSummary[] | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const [detail, setDetail] = useState<Record<number, RequestDetail | "gone">>({});
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const { data } = await apiClient.get<{ requests: RequestSummary[] }>("/api/settings/integration/requests", { params: { limit: 50 } });
      setRows(data.requests);
      setError("");
    } catch (err) {
      const e = err as { response?: { data?: { error?: string } }; message?: string };
      setError(e?.response?.data?.error || e?.message || "載入失敗");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  const toggle = async (id: number) => {
    if (open === id) {
      setOpen(null);
      return;
    }
    setOpen(id);
    if (detail[id]) return;
    try {
      const { data } = await apiClient.get<RequestDetail>(`/api/settings/integration/requests/${id}`);
      setDetail((d) => ({ ...d, [id]: data }));
    } catch {
      setDetail((d) => ({ ...d, [id]: "gone" }));
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <p className="text-xs text-base-content/60 flex-1">
          最近 {rows?.length ?? 0} 筆呼叫（只存在伺服器記憶體，重啟後清空；內容不含 API key）。點一列看傳入與傳出的內容。
        </p>
        <button onClick={load} disabled={loading} className="btn btn-outline btn-xs gap-1">
          <RefreshCw size={12} className={loading ? "animate-spin" : ""} />重新整理
        </button>
      </div>
      {error && <p className="text-sm text-error">{error}</p>}
      <div className="overflow-x-auto">
        <table className="table table-xs">
          <thead>
            <tr><th /><th>時間</th><th>請求</th><th>狀態</th><th>錯誤代碼</th><th>呼叫端</th><th>來源 IP</th><th className="text-right">耗時</th></tr>
          </thead>
          <tbody>
            {rows && rows.length === 0 && (
              <tr><td colSpan={8} className="text-center text-base-content/50">還沒有任何呼叫</td></tr>
            )}
            {rows?.map((r) => (
              <Fragment key={r.id}>
                <tr className="hover cursor-pointer" onClick={() => toggle(r.id)}>
                  <td className="w-6">{open === r.id ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</td>
                  <td className="whitespace-nowrap font-mono">{clock(r.time)}</td>
                  <td className="font-mono">{r.method} {r.path}</td>
                  <td><span className={`badge badge-xs ${statusBadge(r.status)}`}>{r.status}</span>{r.replayed && <span className="badge badge-xs badge-info badge-outline ml-1">重送</span>}</td>
                  <td className="font-mono text-error">{r.error_code}</td>
                  <td>{r.client || <span className="opacity-40">-</span>}</td>
                  <td className="font-mono">{r.ip}</td>
                  <td className="text-right">{r.duration_ms} ms</td>
                </tr>
                {open === r.id && (
                  <tr>
                    <td colSpan={8} className="bg-base-200/40 p-2">
                      {!detail[r.id] ? <span className="loading loading-spinner loading-xs" />
                        : detail[r.id] === "gone" ? <p className="text-sm text-base-content/60">找不到這筆內容（已被較新的紀錄擠掉，或服務已重啟）。</p>
                        : <RequestCard r={detail[r.id] as RequestDetail} />}
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
