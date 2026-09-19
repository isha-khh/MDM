import { useEventStore } from "../stores/eventStore";
import { useTranslation } from "react-i18next";
import { X, Clock, CheckCircle, AlertCircle, Trash2 } from "lucide-react";

interface CommandTrackerProps {
  open: boolean;
  onClose: () => void;
}

export function CommandTracker({ open, onClose }: CommandTrackerProps) {
  const { t } = useTranslation();
  // Selectors — see Layout.tsx for why a bare useEventStore() is worth
  // avoiding here (re-renders on every incoming MDM event, not just when
  // trackedCommands actually changes).
  const trackedCommands = useEventStore((s) => s.trackedCommands);
  const dismissCommand = useEventStore((s) => s.dismissCommand);
  const clearCompletedCommands = useEventStore((s) => s.clearCompletedCommands);

  const statusIcon = {
    sent: <Clock size={14} className="text-warning animate-pulse" />,
    acknowledged: <CheckCircle size={14} className="text-success" />,
    error: <AlertCircle size={14} className="text-error" />,
  };

  const statusBadge = {
    sent: "badge-warning",
    acknowledged: "badge-success",
    error: "badge-error",
  };

  const statusLabel = {
    sent: t("commandTracker.sent"),
    acknowledged: t("commandTracker.acknowledged"),
    error: t("commandTracker.error"),
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end" onClick={onClose}>
      <div className="bg-black/20 absolute inset-0" />
      <div
        className="relative bg-base-100 w-96 max-w-full h-full shadow-xl flex flex-col animate-slide-in-right"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-base-300">
          <h2 className="font-bold text-lg">{t("commandTracker.title")}</h2>
          <div className="flex gap-1">
            {trackedCommands.some((c) => c.status !== "sent") && (
              <button onClick={clearCompletedCommands} className="btn btn-ghost btn-xs gap-1">
                <Trash2 size={12} />
                {t("commandTracker.clearCompleted")}
              </button>
            )}
            <button onClick={onClose} className="btn btn-ghost btn-sm btn-circle">
              <X size={18} />
            </button>
          </div>
        </div>

        {/* Command list */}
        <div className="flex-1 overflow-y-auto">
          {trackedCommands.length === 0 ? (
            <div className="text-center py-12 text-base-content/50 text-sm">
              {t("commandTracker.empty")}
            </div>
          ) : (
            <ul className="divide-y divide-base-200">
              {trackedCommands.map((cmd) => (
                <li key={cmd.id} className="p-3 hover:bg-base-200/50">
                  <div className="flex items-start gap-3">
                    <div className="mt-0.5">{statusIcon[cmd.status]}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm truncate">{cmd.label}</span>
                        <span className={`badge badge-xs ${statusBadge[cmd.status]}`}>
                          {statusLabel[cmd.status]}
                        </span>
                      </div>
                      <div className="text-xs text-base-content/50 mt-0.5">
                        {cmd.udids.length} device(s) · {cmd.sentAt.toLocaleTimeString()}
                      </div>
                      {cmd.responseAt && (
                        <div className="text-xs text-base-content/40 mt-0.5">
                          {t("commandTracker.respondedAt")} {cmd.responseAt.toLocaleTimeString()}
                        </div>
                      )}
                    </div>
                    <button
                      onClick={() => dismissCommand(cmd.id)}
                      className="btn btn-ghost btn-xs btn-circle opacity-50 hover:opacity-100"
                    >
                      <X size={12} />
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
