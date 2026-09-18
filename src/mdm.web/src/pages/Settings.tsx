import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Mail, Inbox, Save, Send, PlugZap, KeyRound, ExternalLink } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "../components/DialogProvider";

interface MailSettings {
  smtp_enabled: boolean;
  smtp_host: string;
  smtp_port: string;
  smtp_username: string;
  smtp_password: string;
  smtp_from: string;
  smtp_from_name: string;
  smtp_tls: boolean;
  /** 額外信任的內部/私有 CA 憑證（PEM），用於郵件伺服器憑證不被系統信任時 */
  smtp_ca_cert: string;
  /** 跳過憑證驗證（不建議，僅在拿不到內部 CA 憑證時作為最後手段） */
  smtp_insecure_skip_verify: boolean;

  incoming_enabled: boolean;
  incoming_protocol: "imap" | "pop3";
  incoming_host: string;
  incoming_port: string;
  incoming_username: string;
  incoming_password: string;
  incoming_tls: boolean;
  incoming_mailbox: string;

  has_smtp_password?: boolean;
  has_incoming_password?: boolean;
  updated_at?: string;
}

const PASSWORD_PLACEHOLDER = "********";

const EMPTY: MailSettings = {
  smtp_enabled: false,
  smtp_host: "",
  smtp_port: "587",
  smtp_username: "",
  smtp_password: "",
  smtp_from: "",
  smtp_from_name: "",
  smtp_tls: true,
  smtp_ca_cert: "",
  smtp_insecure_skip_verify: false,
  incoming_enabled: false,
  incoming_protocol: "imap",
  incoming_host: "",
  incoming_port: "993",
  incoming_username: "",
  incoming_password: "",
  incoming_tls: true,
  incoming_mailbox: "INBOX",
};

interface SSOSettingsForm {
  enabled: boolean;
  issuer_url: string;
  client_id: string;
  client_secret: string;
  redirect_url: string;
  has_client_secret?: boolean;
  updated_at?: string;
}

const SSO_EMPTY: SSOSettingsForm = {
  enabled: false,
  issuer_url: "",
  client_id: "",
  client_secret: "",
  redirect_url: "",
};

export function Settings() {
  const { t } = useTranslation();
  const dialog = useDialog();

  const [form, setForm] = useState<MailSettings>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testingSmtp, setTestingSmtp] = useState(false);
  const [testingIncoming, setTestingIncoming] = useState(false);
  const [testTo, setTestTo] = useState("");

  const [ssoForm, setSSOForm] = useState<SSOSettingsForm>(SSO_EMPTY);
  const [ssoSaving, setSSOSaving] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const [mailResp, ssoResp] = await Promise.all([
        apiClient.get<MailSettings>("/api/settings/mail"),
        apiClient.get<SSOSettingsForm>("/api/settings/sso").catch(() => ({ data: SSO_EMPTY })),
      ]);
      setForm({
        ...mailResp.data,
        smtp_password: mailResp.data.has_smtp_password ? PASSWORD_PLACEHOLDER : "",
        incoming_password: mailResp.data.has_incoming_password ? PASSWORD_PLACEHOLDER : "",
      });
      setSSOForm({
        ...ssoResp.data,
        client_secret: ssoResp.data.has_client_secret ? PASSWORD_PLACEHOLDER : "",
      });
    } catch (err: any) {
      await dialog.error(t("settings.loadFailed") + ": " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { load(); }, []);

  const ssoUpdate = <K extends keyof SSOSettingsForm>(key: K, value: SSOSettingsForm[K]) =>
    setSSOForm((prev) => ({ ...prev, [key]: value }));

  const saveSSOSettings = async () => {
    setSSOSaving(true);
    try {
      await apiClient.put("/api/settings/sso", ssoForm);
      await dialog.alert("SSO 設定已儲存");
      load();
    } catch (err: any) {
      await dialog.error("儲存失敗: " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setSSOSaving(false);
    }
  };

  const update = <K extends keyof MailSettings>(key: K, value: MailSettings[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      await apiClient.put("/api/settings/mail", form);
      await dialog.alert(t("settings.saveOk"));
      load();
    } catch (err: any) {
      await dialog.error(t("settings.saveFailed") + ": " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setSaving(false);
    }
  };

  const testSmtp = async () => {
    if (!testTo) { await dialog.error(t("settings.testToRequired")); return; }
    setTestingSmtp(true);
    try {
      await apiClient.post("/api/settings/mail/test-smtp", { to: testTo });
      await dialog.alert(t("settings.testSmtpOk", { to: testTo }));
    } catch (err: any) {
      await dialog.error(t("settings.testSmtpFailed") + ": " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setTestingSmtp(false);
    }
  };

  const testIncoming = async () => {
    setTestingIncoming(true);
    try {
      const { data } = await apiClient.post("/api/settings/mail/test-incoming", {});
      const greet = data?.greeting ? "\n" + data.greeting.trim() : "";
      await dialog.alert(t("settings.testIncomingOk", { address: data?.address }) + greet);
    } catch (err: any) {
      await dialog.error(t("settings.testIncomingFailed") + ": " + (err?.response?.data?.error || err?.message || ""));
    } finally {
      setTestingIncoming(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-16">
        <span className="loading loading-spinner loading-lg text-primary"></span>
      </div>
    );
  }

  return (
    <div className="space-y-4 max-w-4xl">
      <div>
        <h1 className="text-2xl font-bold">{t("nav.settings")}</h1>
        <p className="text-sm text-base-content/60">{t("settings.subtitle")}</p>
      </div>

      {/* Outgoing (SMTP) */}
      <div className="card bg-base-100 shadow">
        <div className="card-body">
          <div className="flex items-center gap-2">
            <Mail size={18} className="text-primary" />
            <h2 className="card-title text-lg">{t("settings.outgoing")}</h2>
            <label className="ml-auto label cursor-pointer gap-2">
              <span className="label-text">{t("settings.enabled")}</span>
              <input
                type="checkbox"
                className="toggle toggle-primary"
                checked={form.smtp_enabled}
                onChange={(e) => update("smtp_enabled", e.target.checked)}
              />
            </label>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <label className="form-control">
              <span className="label label-text">{t("settings.host")}</span>
              <input className="input input-bordered input-sm" value={form.smtp_host}
                onChange={(e) => update("smtp_host", e.target.value)} placeholder="smtp.gmail.com" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.port")}</span>
              <input className="input input-bordered input-sm" value={form.smtp_port}
                onChange={(e) => update("smtp_port", e.target.value)} placeholder="587" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.username")}</span>
              <input className="input input-bordered input-sm" value={form.smtp_username}
                onChange={(e) => update("smtp_username", e.target.value)} autoComplete="off" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.password")}</span>
              <input className="input input-bordered input-sm" type="password" value={form.smtp_password}
                onChange={(e) => update("smtp_password", e.target.value)} autoComplete="new-password"
                placeholder={form.has_smtp_password ? t("settings.passwordSet") : ""} />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.from")}</span>
              <input className="input input-bordered input-sm" value={form.smtp_from}
                onChange={(e) => update("smtp_from", e.target.value)} placeholder="noreply@example.com" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.fromName")}</span>
              <input className="input input-bordered input-sm" value={form.smtp_from_name}
                onChange={(e) => update("smtp_from_name", e.target.value)} placeholder="MDM 管理平台" />
            </label>
            <label className="label cursor-pointer justify-start gap-2 md:col-span-2">
              <input type="checkbox" className="checkbox checkbox-sm"
                checked={form.smtp_tls}
                onChange={(e) => update("smtp_tls", e.target.checked)} />
              <span className="label-text">{t("settings.useTls")}</span>
            </label>
          </div>

          {form.smtp_tls && (
            <div className="mt-2 space-y-2">
              <label className="form-control">
                <span className="label label-text">
                  內部憑證機構（CA）憑證 <span className="opacity-50 font-normal">（選填，PEM 格式）</span>
                </span>
                <textarea
                  className="textarea textarea-bordered textarea-sm font-mono text-xs"
                  rows={4}
                  value={form.smtp_ca_cert}
                  onChange={(e) => update("smtp_ca_cert", e.target.value)}
                  placeholder={"-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"}
                />
                <span className="label">
                  <span className="label-text-alt opacity-60">
                    若郵件伺服器使用內部/自簽憑證，導致寄信失敗（certificate signed by unknown authority），
                    請向 IT 索取該憑證機構的 CA 憑證檔並貼在這裡，系統會額外信任它，其他驗證邏輯不變。
                  </span>
                </span>
              </label>
              <label className="label cursor-pointer justify-start gap-2">
                <input type="checkbox" className="checkbox checkbox-sm checkbox-warning"
                  checked={form.smtp_insecure_skip_verify}
                  onChange={(e) => update("smtp_insecure_skip_verify", e.target.checked)} />
                <span className="label-text">
                  跳過憑證驗證 <span className="text-warning">（不建議，拿不到 CA 憑證時的最後手段）</span>
                </span>
              </label>
              {form.smtp_insecure_skip_verify && (
                <div role="alert" className="alert alert-warning py-2">
                  <span className="text-sm">
                    這會完全關閉郵件伺服器的憑證驗證，可能遭受中間人攻擊竊取帳密。請優先使用上方的 CA 憑證欄位。
                  </span>
                </div>
              )}
            </div>
          )}

          <div className="divider my-2"></div>
          <div className="flex flex-wrap items-end gap-2">
            <label className="form-control flex-1 min-w-48">
              <span className="label label-text">{t("settings.testTo")}</span>
              <input className="input input-bordered input-sm" type="email" value={testTo}
                onChange={(e) => setTestTo(e.target.value)} />
            </label>
            <button onClick={testSmtp} disabled={testingSmtp || !form.smtp_enabled}
              className="btn btn-outline btn-sm gap-1">
              {testingSmtp
                ? <span className="loading loading-spinner loading-xs"></span>
                : <Send size={14} />}
              {t("settings.testSmtp")}
            </button>
          </div>
        </div>
      </div>

      {/* Incoming (IMAP/POP3) */}
      <div className="card bg-base-100 shadow">
        <div className="card-body">
          <div className="flex items-center gap-2">
            <Inbox size={18} className="text-primary" />
            <h2 className="card-title text-lg">{t("settings.incoming")}</h2>
            <label className="ml-auto label cursor-pointer gap-2">
              <span className="label-text">{t("settings.enabled")}</span>
              <input
                type="checkbox"
                className="toggle toggle-primary"
                checked={form.incoming_enabled}
                onChange={(e) => update("incoming_enabled", e.target.checked)}
              />
            </label>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <label className="form-control">
              <span className="label label-text">{t("settings.protocol")}</span>
              <select className="select select-bordered select-sm"
                value={form.incoming_protocol}
                onChange={(e) => update("incoming_protocol", e.target.value as "imap" | "pop3")}>
                <option value="imap">IMAP</option>
                <option value="pop3">POP3</option>
              </select>
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.mailbox")}</span>
              <input className="input input-bordered input-sm" value={form.incoming_mailbox}
                onChange={(e) => update("incoming_mailbox", e.target.value)} placeholder="INBOX" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.host")}</span>
              <input className="input input-bordered input-sm" value={form.incoming_host}
                onChange={(e) => update("incoming_host", e.target.value)} placeholder="imap.gmail.com" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.port")}</span>
              <input className="input input-bordered input-sm" value={form.incoming_port}
                onChange={(e) => update("incoming_port", e.target.value)} placeholder="993" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.username")}</span>
              <input className="input input-bordered input-sm" value={form.incoming_username}
                onChange={(e) => update("incoming_username", e.target.value)} autoComplete="off" />
            </label>
            <label className="form-control">
              <span className="label label-text">{t("settings.password")}</span>
              <input className="input input-bordered input-sm" type="password" value={form.incoming_password}
                onChange={(e) => update("incoming_password", e.target.value)} autoComplete="new-password"
                placeholder={form.has_incoming_password ? t("settings.passwordSet") : ""} />
            </label>
            <label className="label cursor-pointer justify-start gap-2 md:col-span-2">
              <input type="checkbox" className="checkbox checkbox-sm"
                checked={form.incoming_tls}
                onChange={(e) => update("incoming_tls", e.target.checked)} />
              <span className="label-text">{t("settings.useTls")}</span>
            </label>
          </div>

          <div className="divider my-2"></div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-base-content/60 flex-1">{t("settings.testIncomingHint")}</span>
            <button onClick={testIncoming} disabled={testingIncoming || !form.incoming_enabled}
              className="btn btn-outline btn-sm gap-1">
              {testingIncoming
                ? <span className="loading loading-spinner loading-xs"></span>
                : <PlugZap size={14} />}
              {t("settings.testIncoming")}
            </button>
          </div>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        {form.updated_at && (
          <span className="text-xs text-base-content/50 self-center">
            {t("settings.updatedAt", { at: new Date(form.updated_at).toLocaleString() })}
          </span>
        )}
        <button onClick={save} disabled={saving} className="btn btn-primary btn-sm gap-1">
          {saving
            ? <span className="loading loading-spinner loading-xs"></span>
            : <Save size={14} />}
          {t("common.save")}
        </button>
      </div>

      {/* SSO / OIDC */}
      <div className="card bg-base-100 shadow">
        <div className="card-body">
          <div className="flex items-center gap-2">
            <KeyRound size={18} className="text-primary" />
            <h2 className="card-title text-lg">SSO 單一登入（OIDC）</h2>
            <label className="ml-auto label cursor-pointer gap-2">
              <span className="label-text">啟用</span>
              <input
                type="checkbox"
                className="toggle toggle-primary"
                checked={ssoForm.enabled}
                onChange={(e) => ssoUpdate("enabled", e.target.checked)}
              />
            </label>
          </div>

          <p className="text-sm text-base-content/60">
            支援任何相容 OpenID Connect 的 IdP（Google、Azure AD、Keycloak、Okta 等）。
            填入後，登入頁面會出現「SSO 登入」按鈕；email 相同的既有帳號會自動綁定。
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-2">
            <label className="form-control md:col-span-2">
              <span className="label label-text">Issuer URL</span>
              <input className="input input-bordered input-sm" value={ssoForm.issuer_url}
                onChange={(e) => ssoUpdate("issuer_url", e.target.value)}
                placeholder="https://accounts.google.com" />
              <span className="label label-text-alt text-base-content/50">
                IdP 的根網址，系統會自動從 /.well-known/openid-configuration 取得端點
              </span>
            </label>
            <label className="form-control">
              <span className="label label-text">Client ID</span>
              <input className="input input-bordered input-sm" value={ssoForm.client_id}
                onChange={(e) => ssoUpdate("client_id", e.target.value)}
                placeholder="your-client-id" autoComplete="off" />
            </label>
            <label className="form-control">
              <span className="label label-text">Client Secret</span>
              <input className="input input-bordered input-sm" type="password" value={ssoForm.client_secret}
                onChange={(e) => ssoUpdate("client_secret", e.target.value)}
                autoComplete="new-password"
                placeholder={ssoForm.has_client_secret ? "已設定（留空保留原值）" : ""} />
            </label>
            <label className="form-control md:col-span-2">
              <span className="label label-text">Redirect URL（回呼網址）</span>
              <div className="flex gap-2">
                <input className="input input-bordered input-sm flex-1" value={ssoForm.redirect_url}
                  onChange={(e) => ssoUpdate("redirect_url", e.target.value)}
                  placeholder="https://mdm.example.com/api/auth/sso/callback" />
                {ssoForm.redirect_url && (
                  <a href={ssoForm.redirect_url} target="_blank" rel="noreferrer"
                    className="btn btn-ghost btn-sm" title="在 IdP 設定此回呼網址">
                    <ExternalLink size={14} />
                  </a>
                )}
              </div>
              <span className="label label-text-alt text-base-content/50">
                需在 IdP 的 Allowed Redirect URIs 填入此網址
              </span>
            </label>
          </div>

          <div className="flex justify-between items-center mt-2">
            {ssoForm.updated_at && ssoForm.updated_at !== "0001-01-01T00:00:00Z" && (
              <span className="text-xs text-base-content/50">
                最後更新：{new Date(ssoForm.updated_at).toLocaleString()}
              </span>
            )}
            <button onClick={saveSSOSettings} disabled={ssoSaving} className="btn btn-primary btn-sm gap-1 ml-auto">
              {ssoSaving
                ? <span className="loading loading-spinner loading-xs"></span>
                : <Save size={14} />}
              儲存 SSO 設定
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
