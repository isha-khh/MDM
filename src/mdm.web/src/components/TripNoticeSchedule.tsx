import { useEffect, useState } from "react";
import { AlertTriangle, RefreshCw, Save } from "lucide-react";
import apiClient from "../lib/apiClient";
import { useDialog } from "./DialogProvider";

interface Schedule {
  enabled: boolean;
  send_hour: number;
  last_run_at?: string;
  last_run_summary?: string;
  problems: string[];
}

function errorText(err: unknown, fallback: string): string {
  const e = err as { response?: { data?: { error?: string } }; message?: string };
  return e?.response?.data?.error || e?.message || fallback;
}

const HOURS = Array.from({ length: 24 }, (_, h) => h);

// Daily automatic sending of the trip rental-car notices. Off by default; the
// notice log guarantees a person who was already notified is never mailed
// again, however often this runs.
interface Props {
  // Runs the same check the schedule does, right now (see GoogleCalendarSettings).
  onRecheck: () => void;
  checking: boolean;
  checkedAt: Date | null;
  canRecheck: boolean;
}

export function TripNoticeSchedule({ onRecheck, checking, checkedAt, canRecheck }: Props) {
  const dialog = useDialog();
  const [sched, setSched] = useState<Schedule | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [hour, setHour] = useState(9);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    apiClient.get<Schedule>("/api/settings/google-calendar/notices/schedule")
      .then(({ data }) => { if (!cancelled) { setSched(data); setEnabled(data.enabled); setHour(data.send_hour); } })
      .catch((err) => { if (!cancelled) setLoadError(errorText(err, "讀取失敗")); });
    return () => { cancelled = true; };
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      const { data } = await apiClient.put<Schedule>("/api/settings/google-calendar/notices/schedule", { enabled, send_hour: hour });
      setSched(data);
      await dialog.success(data.enabled
        ? `已開啟：每天 ${String(data.send_hour).padStart(2, "0")}:00（台灣時間）之後自動寄出到期的通知`
        : "已關閉自動寄送");
    } catch (err) {
      await dialog.error("儲存失敗：" + errorText(err, ""));
    } finally {
      setSaving(false);
    }
  };

  const dirty = !!sched && (sched.enabled !== enabled || sched.send_hour !== hour);

  return (
    <section className="space-y-2">
      <h3 className="font-medium text-sm">⑤ 每日自動寄送</h3>
      <p className="text-xs text-base-content/50">
        開啟後，每天到了設定的時間（台灣時間）自動寄出到期的借車通知，條件與上面手動「寄出」完全相同。已通知過的人不會再寄；寄失敗的隔天會再試一次。預設關閉。
      </p>
      {loadError && <div className="alert alert-error text-sm">{loadError}</div>}
      {sched && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-3">
            <label className="label cursor-pointer gap-2 p-0">
              <input type="checkbox" className="toggle toggle-sm toggle-primary" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              <span className="label-text text-sm">自動寄送</span>
            </label>
            <label className="flex items-center gap-1 text-sm">
              每天
              <select className="select select-bordered select-xs" value={hour} onChange={(e) => setHour(Number(e.target.value))}>
                {HOURS.map((h) => <option key={h} value={h}>{String(h).padStart(2, "0")}:00</option>)}
              </select>
              之後
            </label>
            <button className="btn btn-sm btn-primary gap-1" disabled={saving || !dirty} onClick={save}>
              {saving ? <span className="loading loading-spinner loading-xs" /> : <Save size={14} />}
              儲存
            </button>
          </div>
          {sched.problems.length > 0 && (
            <div className="alert alert-warning text-xs">
              <AlertTriangle size={14} />
              <div>
                <div className="font-medium">目前這些設定不齊，自動寄送會跑不起來：</div>
                <ul className="list-disc ml-4">{sched.problems.map((p) => <li key={p}>{p}</li>)}</ul>
              </div>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-3">
            <button className="btn btn-sm btn-outline gap-1" disabled={checking || !canRecheck} onClick={onRecheck}>
              {checking ? <span className="loading loading-spinner loading-xs" /> : <RefreshCw size={14} />}
              立即重新檢查
            </button>
            <span className="text-xs text-base-content/50">
              {canRecheck
                ? "不用等排程：馬上重新讀取日曆與人員目錄，更新上方預覽；有待通知的人會先列出讓你確認再寄。"
                : "先選擇並儲存要讀取的日曆"}
              {checkedAt && `；上次檢查：${checkedAt.toLocaleTimeString()}`}
            </span>
          </div>
          {sched.last_run_at && (
            <p className="text-xs text-base-content/60">
              上次執行：{new Date(sched.last_run_at).toLocaleString()}；{sched.last_run_summary}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
