import { useEffect, useState } from "react";
import { Copy, KeyRound, Plug, Plus, RefreshCw, Save, Trash2 } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "./DialogProvider";

interface IntegrationClient {
  name: string;
  key_prefix: string;
  enabled: boolean;
  ip_allowlist: string[];
  auto_create_email_domains: string[];
  created_at: string;
  rotated_at: string | null;
  last_used_at: string | null;
}

interface IntegrationResource {
  resource_id: string;
  service_type: string;
  asset_id: string;
  asset_number: string;
  asset_name: string;
  allowed_cities: string[];
  enabled: boolean;
}

interface IntegrationState {
  listener_enabled: boolean;
  listen_addr: string;
  client: IntegrationClient | null;
  resources: IntegrationResource[];
}

interface AssetOption {
  id: string;
  asset_number: string;
  name: string;
  is_rentable: boolean;
}

const DEFAULT_CITIES = "台南市, 高雄市, 屏東縣";

const lines = (s: string) => s.split(/[\n,]/).map((x) => x.trim()).filter(Boolean);
const fmt = (s: string | null) => (s ? new Date(s).toLocaleString() : "—");
const errText = (err: unknown) => {
  const e = err as { response?: { data?: { error?: string } }; message?: string };
  return e?.response?.data?.error || e?.message || "";
};

/** 派發任務系統整合 API：API key、來源限制、資源（車輛）對應。 */
export function IntegrationSettings() {
  const dialog = useDialog();
  const [state, setState] = useState<IntegrationState | null>(null);
  const [assets, setAssets] = useState<AssetOption[]>([]);
  const [busy, setBusy] = useState(false);

  // 剛發行的 key：只在這裡顯示一次
  const [newKey, setNewKey] = useState<string | null>(null);

  const [ipText, setIpText] = useState("");
  const [domainText, setDomainText] = useState("");

  const [resId, setResId] = useState("RENTAL_CAR_01");
  const [resAsset, setResAsset] = useState("");
  const [resCities, setResCities] = useState(DEFAULT_CITIES);

  const adopt = (s: IntegrationState) => {
    setState(s);
    setIpText((s.client?.ip_allowlist || []).join("\n"));
    setDomainText((s.client?.auto_create_email_domains || []).join("\n"));
  };

  const load = async () => {
    try {
      const { data } = await apiClient.get<IntegrationState>("/api/settings/integration");
      adopt(data);
    } catch (err) {
      await dialog.error("載入整合 API 設定失敗: " + errText(err));
    }
  };

  useEffect(() => {
    load();
    apiClient.get("/api/assets")
      .then(({ data }) => setAssets((data.assets || []).filter((a: AssetOption) => a.is_rentable)))
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const run = async (fn: () => Promise<void>, failure: string) => {
    setBusy(true);
    try {
      await fn();
    } catch (err) {
      await dialog.error(failure + ": " + errText(err));
    } finally {
      setBusy(false);
    }
  };

  const issueKey = () => run(async () => {
    const rotating = !!state?.client;
    if (rotating && !(await dialog.confirm(
      "輪替後舊的 API key 會立刻失效，派發系統必須換成新的 key 才能繼續呼叫。確定要輪替嗎？"))) return;
    const { data } = await apiClient.post("/api/settings/integration/key");
    setNewKey(data.key);
    await load();
  }, "發行 API key 失敗");

  const copyKey = async () => {
    if (!newKey) return;
    try {
      await navigator.clipboard.writeText(newKey);
      await dialog.success("已複製");
    } catch {
      await dialog.alert("無法自動複製，請手動選取 key 複製");
    }
  };

  const saveLimits = () => run(async () => {
    const { data } = await apiClient.put<IntegrationState>("/api/settings/integration", {
      ip_allowlist: lines(ipText),
      auto_create_email_domains: lines(domainText),
    });
    adopt(data);
    await dialog.success("已儲存");
  }, "儲存失敗");

  const toggleEnabled = (enabled: boolean) => run(async () => {
    const { data } = await apiClient.put<IntegrationState>("/api/settings/integration", { enabled });
    adopt(data);
  }, "更新失敗");

  const saveResource = () => run(async () => {
    if (!resId.trim() || !resAsset) {
      await dialog.alert("請填資源代號並選擇車輛資產");
      return;
    }
    const { data } = await apiClient.put<IntegrationState>(
      `/api/settings/integration/resources/${encodeURIComponent(resId.trim())}`,
      { asset_id: resAsset, allowed_cities: lines(resCities), enabled: true });
    adopt(data);
    setResAsset("");
  }, "儲存資源失敗");

  const setResourceEnabled = (r: IntegrationResource, enabled: boolean) => run(async () => {
    const { data } = await apiClient.put<IntegrationState>(
      `/api/settings/integration/resources/${encodeURIComponent(r.resource_id)}`,
      { asset_id: r.asset_id, service_type: r.service_type, allowed_cities: r.allowed_cities, enabled });
    adopt(data);
  }, "更新資源失敗");

  const removeResource = (r: IntegrationResource) => run(async () => {
    if (!(await dialog.confirm(
      `移除 ${r.resource_id} 的對應後，派發系統將查不到這輛車已建立的預約（租借紀錄本身不受影響）。` +
      "若只是暫時不開放，請改用停用。確定要移除嗎？"))) return;
    const { data } = await apiClient.delete<IntegrationState>(
      `/api/settings/integration/resources/${encodeURIComponent(r.resource_id)}`);
    adopt(data);
  }, "移除資源失敗");

  if (!state) {
    return (
      <div className="card bg-base-100 shadow">
        <div className="card-body"><span className="loading loading-spinner loading-sm" /></div>
      </div>
    );
  }
  const client = state.client;

  return (
    <div className="card bg-base-100 shadow">
      <div className="card-body">
        <div className="flex items-center gap-2">
          <Plug size={18} className="text-primary" />
          <h2 className="card-title text-lg">派發系統整合 API</h2>
          {state.listener_enabled
            ? <span className="badge badge-success badge-sm">服務中 {state.listen_addr}</span>
            : <span className="badge badge-warning badge-sm">未啟用</span>}
        </div>
        <p className="text-sm text-base-content/60">
          讓派發任務系統預約公司租賃車輛。API 跑在獨立連接埠，不含網頁；建議只透過 Cloudflare Tunnel 發佈這個連接埠。
          詳見 docs/dispatch-integration.md。
        </p>
        {!state.listener_enabled && (
          <div className="alert alert-warning text-sm py-2">
            伺服器沒有設定 <code>INTEGRATION_LISTEN_ADDR</code>（例如 <code>:8081</code>），整合 API 目前不會啟動。
            設定後需重啟 API 服務；下面的 key 與車輛對應可以先設好。
          </div>
        )}

        {/* API key */}
        <div className="divider my-1 text-sm">API key</div>
        {newKey && (
          <div className="rounded-lg border border-success bg-success/10 p-3 text-sm space-y-2">
            <p>新的 API key（<b>只會顯示這一次</b>，離開或重新整理後就看不到了，請立即交給派發系統）：</p>
            <div className="flex flex-wrap gap-2">
              <input readOnly value={newKey} onFocus={(e) => e.target.select()}
                className="input input-bordered input-sm flex-1 min-w-[16rem] font-mono text-xs bg-base-100" />
              <button onClick={copyKey} className="btn btn-sm gap-1"><Copy size={14} />複製</button>
              <button onClick={() => setNewKey(null)} className="btn btn-ghost btn-sm">我已保存</button>
            </div>
          </div>
        )}
        {client ? (
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
            <span>前綴 <code className="font-mono">{client.key_prefix}…</code></span>
            <span className="text-base-content/60">發行 {fmt(client.created_at)}</span>
            {client.rotated_at && <span className="text-base-content/60">上次輪替 {fmt(client.rotated_at)}</span>}
            <span className="text-base-content/60">最後使用 {fmt(client.last_used_at)}</span>
            <label className="label cursor-pointer gap-2 ml-auto">
              <span className="label-text">啟用</span>
              <input type="checkbox" className="toggle toggle-primary toggle-sm" checked={client.enabled}
                disabled={busy} onChange={(e) => toggleEnabled(e.target.checked)} />
            </label>
            <button onClick={issueKey} disabled={busy} className="btn btn-outline btn-sm gap-1">
              <RefreshCw size={14} />輪替 key
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-3">
            <span className="text-sm text-base-content/60">尚未發行 API key。</span>
            <button onClick={issueKey} disabled={busy} className="btn btn-primary btn-sm gap-1">
              <KeyRound size={14} />發行 API key
            </button>
          </div>
        )}

        {/* 來源與帳號 */}
        {client && (
          <>
            <div className="divider my-1 text-sm">來源限制與借用人帳號</div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <label className="form-control">
                <span className="label label-text">IP 白名單（一行一個，IP 或 CIDR）</span>
                <textarea className="textarea textarea-bordered textarea-sm font-mono h-24" value={ipText}
                  onChange={(e) => setIpText(e.target.value)} placeholder={"203.0.113.10\n198.51.100.0/24"} />
                <span className="label label-text-alt text-base-content/50">
                  留空 = 不限來源。經 Cloudflare Tunnel 時比對的是 CF-Connecting-IP。
                </span>
              </label>
              <label className="form-control">
                <span className="label label-text">可自動建立借用人帳號的 email 網域（一行一個）</span>
                <textarea className="textarea textarea-bordered textarea-sm font-mono h-24" value={domainText}
                  onChange={(e) => setDomainText(e.target.value)} placeholder="mail.isha.org.tw" />
                <span className="label label-text-alt text-base-content/50">
                  預約的借用人在 MDM 找不到時，若其 email 屬於這些網域就自動建立帳號（日後以 SSO 登入）；留空 = 一律不建立。
                </span>
              </label>
            </div>
            <div className="flex justify-end">
              <button onClick={saveLimits} disabled={busy} className="btn btn-primary btn-sm gap-1">
                <Save size={14} />儲存
              </button>
            </div>
          </>
        )}

        {/* 資源對應 */}
        <div className="divider my-1 text-sm">資源（車輛）對應</div>
        <p className="text-xs text-base-content/60">
          派發系統用資源代號（如 RENTAL_CAR_01）指定車輛。車輛所屬的分類請到「分類管理」打開「可預約」，
          網頁上的借用才不會被日後的預約擋住。
        </p>
        <div className="overflow-x-auto">
          <table className="table table-sm">
            <thead>
              <tr><th>資源代號</th><th>資產</th><th>可預約縣市</th><th>啟用</th><th /></tr>
            </thead>
            <tbody>
              {state.resources.length === 0 && (
                <tr><td colSpan={5} className="text-center text-base-content/50">尚未對應任何資源</td></tr>
              )}
              {state.resources.map((r) => (
                <tr key={r.resource_id}>
                  <td className="font-mono">{r.resource_id}</td>
                  <td>{r.asset_number} {r.asset_name}</td>
                  <td>{r.allowed_cities.length ? r.allowed_cities.join("、") : "不限"}</td>
                  <td>
                    <input type="checkbox" className="toggle toggle-sm toggle-primary" checked={r.enabled}
                      disabled={busy} onChange={(e) => setResourceEnabled(r, e.target.checked)} />
                  </td>
                  <td className="text-right">
                    <button onClick={() => removeResource(r)} disabled={busy}
                      className="btn btn-ghost btn-xs text-error" title="移除對應"><Trash2 size={14} /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-[12rem_1fr_1fr_auto] gap-2 items-end">
          <label className="form-control">
            <span className="label label-text">資源代號</span>
            <input className="input input-bordered input-sm font-mono" value={resId}
              onChange={(e) => setResId(e.target.value)} placeholder="RENTAL_CAR_01" />
          </label>
          <label className="form-control">
            <span className="label label-text">車輛資產（僅列出可租借的資產）</span>
            <select className="select select-bordered select-sm" value={resAsset}
              onChange={(e) => setResAsset(e.target.value)}>
              <option value="">請選擇</option>
              {assets.map((a) => <option key={a.id} value={a.id}>{`${a.asset_number} ${a.name}`.trim()}</option>)}
            </select>
          </label>
          <label className="form-control">
            <span className="label label-text">可預約縣市（逗號分隔，留空 = 不限）</span>
            <input className="input input-bordered input-sm" value={resCities}
              onChange={(e) => setResCities(e.target.value)} />
          </label>
          <button onClick={saveResource} disabled={busy} className="btn btn-primary btn-sm gap-1">
            <Plus size={14} />新增／更新
          </button>
        </div>
      </div>
    </div>
  );
}
