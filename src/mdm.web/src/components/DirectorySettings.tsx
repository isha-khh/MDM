import { useEffect, useState } from "react";
import { Users, AlertTriangle, PlugZap, Save, Trash2 } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "./DialogProvider";

interface DirectoryStatus {
  encryption_ready: boolean;
  encryption_error?: string;
  base_url: string;
  has_api_key: boolean;
  updated_at?: string;
}

interface Tri { yes: number; no: number; unknown: number }

interface DirectoryTestResult {
  total_accounts: number;
  enabled_accounts: number;
  people: number;
  has_license: Tri;
  has_own_car: Tri;
}

function errorText(err: unknown, fallback: string): string {
  const e = err as { response?: { data?: { error?: string } }; message?: string };
  return e?.response?.data?.error || e?.message || fallback;
}

function TriRow({ label, t, total }: { label: string; t: Tri; total: number }) {
  return (
    <tr>
      <td>{label}</td>
      <td>{t.yes}</td>
      <td>{t.no}</td>
      <td className={t.unknown > 0 ? "text-warning font-medium" : ""}>{t.unknown}</td>
      <td className="opacity-60">{total ? Math.round((t.unknown / total) * 100) : 0}%</td>
    </tr>
  );
}

// Connection to the company user directory, which knows who holds a driver's
// license and who owns a car. The API key is write-only: once saved it can be
// replaced but never read back.
export function DirectorySettings() {
  const dialog = useDialog();
  const [status, setStatus] = useState<DirectoryStatus | null>(null);
  const [loadError, setLoadError] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<DirectoryTestResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    apiClient.get<DirectoryStatus>("/api/settings/directory")
      .then(({ data }) => { if (!cancelled) { setStatus(data); setBaseURL(data.base_url); } })
      .catch((err) => { if (!cancelled) setLoadError(errorText(err, "讀取失敗")); });
    return () => { cancelled = true; };
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      const { data } = await apiClient.put<DirectoryStatus>("/api/settings/directory", { base_url: baseURL, api_key: apiKey });
      setStatus(data);
      setBaseURL(data.base_url);
      setApiKey("");
      setResult(null);
      await dialog.success("已加密儲存。按「測試連線」確認讀得到資料");
    } catch (err) {
      await dialog.error("儲存失敗：" + errorText(err, ""));
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    setTesting(true);
    setResult(null);
    try {
      const { data } = await apiClient.post<DirectoryTestResult>("/api/settings/directory/test");
      setResult(data);
    } catch (err) {
      await dialog.error(errorText(err, "測試連線失敗"));
    } finally {
      setTesting(false);
    }
  };

  const remove = async () => {
    if (!(await dialog.confirm("移除人員目錄的連線設定與 API key？"))) return;
    try {
      await apiClient.delete("/api/settings/directory");
      const { data } = await apiClient.get<DirectoryStatus>("/api/settings/directory");
      setStatus(data);
      setBaseURL("");
      setApiKey("");
      setResult(null);
    } catch (err) {
      await dialog.error(errorText(err, "移除失敗"));
    }
  };

  const configured = !!status && status.base_url !== "" && status.has_api_key;
  const canSave = !!status?.encryption_ready && baseURL.trim() !== "" && (apiKey.trim() !== "" || status.has_api_key);

  return (
    <div className="card bg-base-100 shadow">
      <div className="card-body">
        <div className="flex items-center gap-2">
          <Users size={18} className="text-primary" />
          <h2 className="card-title text-lg">人員目錄（駕照／自有車輛）</h2>
          <div className="ml-auto">
            {status && (configured
              ? <span className="badge badge-success badge-sm">已設定</span>
              : <span className="badge badge-ghost badge-sm">尚未設定</span>)}
          </div>
        </div>
        <p className="text-sm text-base-content/60">
          出差借車通知用它比對出差人員：有駕照才通知、沒有自己車的優先。只讀取，API key 加密儲存且不會再顯示。
        </p>

        {loadError && <div className="alert alert-error text-sm">{loadError}</div>}

        {status && !status.encryption_ready && (
          <div className="alert alert-warning text-sm">
            <AlertTriangle size={16} />
            <div>
              <div className="font-medium">{status.encryption_error}</div>
              <div className="opacity-80">在後端 .env 加入 <code>SETTINGS_ENCRYPTION_KEY</code> 並重啟後才能儲存 API key。</div>
            </div>
          </div>
        )}

        {status && (
          <div className="space-y-3 mt-1">
            <label className="form-control">
              <span className="label-text text-sm mb-1">服務網址</span>
              <input className="input input-bordered input-sm font-mono" placeholder="http://10.6.20.14:8089"
                value={baseURL} onChange={(e) => setBaseURL(e.target.value)} />
              <span className="text-xs text-base-content/50 mt-1">可以貼完整的 <code>…/api/users</code>，會自動取到主機位址。</span>
            </label>
            <label className="form-control">
              <span className="label-text text-sm mb-1">API key</span>
              <input type="password" autoComplete="off" className="input input-bordered input-sm font-mono"
                placeholder={status.has_api_key ? "已儲存（留空則沿用，輸入新值會取代）" : "貼上 X-API-Key"}
                value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
            </label>

            <div className="flex flex-wrap gap-2">
              <button className="btn btn-sm btn-primary gap-1" disabled={saving || !canSave} onClick={save}>
                {saving ? <span className="loading loading-spinner loading-xs" /> : <Save size={14} />}
                儲存
              </button>
              <button className="btn btn-sm btn-outline gap-1" disabled={testing || !configured} onClick={test}>
                {testing ? <span className="loading loading-spinner loading-xs" /> : <PlugZap size={14} />}
                測試連線
              </button>
              {configured && (
                <button className="btn btn-sm btn-ghost text-error gap-1 ml-auto" onClick={remove}><Trash2 size={14} /> 移除</button>
              )}
            </div>

            {result && (
              <div className="rounded-lg border border-base-300 p-3 text-sm space-y-2">
                <div>
                  連線成功：共 {result.total_accounts} 個帳號，啟用 {result.enabled_accounts} 個，其中有信箱的人員 {result.people} 位。
                </div>
                <table className="table table-xs w-auto">
                  <thead><tr><th>人員（有信箱）</th><th>是</th><th>否</th><th>未填</th><th>未填比例</th></tr></thead>
                  <tbody>
                    <TriRow label="有駕照" t={result.has_license} total={result.people} />
                    <TriRow label="有自己的車" t={result.has_own_car} total={result.people} />
                  </tbody>
                </table>
                {(result.has_license.unknown > 0 || result.has_own_car.unknown > 0) && (
                  <p className="text-xs text-base-content/50">
                    「未填」的人不會被寄信，會在預覽中標為「資料不全」，待後台補齊。
                  </p>
                )}
              </div>
            )}
          </div>
        )}

        {status?.updated_at && status.updated_at !== "0001-01-01T00:00:00Z" && (
          <span className="text-xs text-base-content/50">最後更新：{new Date(status.updated_at).toLocaleString()}</span>
        )}
      </div>
    </div>
  );
}
