import { useEventStore } from "../stores/eventStore";
import { X, CheckCircle, AlertCircle, Info } from "lucide-react";

export function ToastContainer() {
  // Selectors, not a whole-store destructure — same reasoning as Layout.tsx:
  // this is mounted at the app root, and a bare useEventStore() would
  // re-render it (harmlessly here, since it early-returns, but still real
  // render work) on every single incoming MDM event, not just when toasts
  // actually change.
  const toasts = useEventStore((s) => s.toasts);
  const dismissToast = useEventStore((s) => s.dismissToast);

  if (toasts.length === 0) return null;

  const icons = {
    success: <CheckCircle size={16} />,
    error: <AlertCircle size={16} />,
    info: <Info size={16} />,
  };

  const alertClass = {
    success: "alert-success",
    error: "alert-error",
    info: "alert-info",
  };

  return (
    <div className="toast toast-end toast-bottom z-50">
      {toasts.slice(-5).map((toast) => (
        <div key={toast.id} className={`alert ${alertClass[toast.type]} shadow-lg py-2 px-4 min-w-64 max-w-sm`}>
          {icons[toast.type]}
          <span className="text-sm flex-1 truncate">{toast.message}</span>
          <button onClick={() => dismissToast(toast.id)} className="btn btn-ghost btn-xs btn-circle">
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  );
}
