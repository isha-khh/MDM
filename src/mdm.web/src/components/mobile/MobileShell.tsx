import { Link } from "react-router";
import { ChevronLeft } from "lucide-react";
import { useOnlineStatus } from "../../lib/offlineQueue";

export function ConnPill() {
  const online = useOnlineStatus();
  return (
    <span className={`badge ${online ? "badge-success" : "badge-warning"} badge-sm whitespace-nowrap gap-1`}>
      {online ? "已連線" : "離線模式"}
    </span>
  );
}

interface MobileShellProps {
  title: string;
  subtitle?: string;
  backTo?: string;
  footer?: React.ReactNode;
  children: React.ReactNode;
}

// Shared chrome for the /m/* borrower self-service pages: no sidebar, a
// sticky header with a back link and connectivity pill, a scrollable body,
// and an optional fixed-bottom action bar (used for the submit buttons).
export function MobileShell({ title, subtitle, backTo, footer, children }: MobileShellProps) {
  return (
    <div className="min-h-screen bg-base-200 flex flex-col">
      <div className="flex-none bg-base-100 border-b border-base-300 px-4 pt-5 pb-3 flex flex-col gap-2 sticky top-0 z-10">
        <div className="flex items-center gap-2">
          {backTo && (
            <Link to={backTo} aria-label="返回" className="btn btn-ghost btn-circle btn-sm">
              <ChevronLeft size={20} />
            </Link>
          )}
          <h1 className="text-lg font-bold flex-1">{title}</h1>
          <ConnPill />
        </div>
        {subtitle && (
          <div className="text-xs text-base-content/60" style={{ paddingLeft: backTo ? 40 : 0 }}>{subtitle}</div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-4">
        {children}
      </div>

      {footer && (
        <div className="flex-none bg-base-100 border-t border-base-300 p-4 flex flex-col gap-2">
          {footer}
        </div>
      )}
    </div>
  );
}
