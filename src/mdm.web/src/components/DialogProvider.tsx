import { createContext, useContext, useState, useCallback, useMemo, useRef } from "react";
import { AlertCircle, CheckCircle, Info } from "lucide-react";
import type { ReactNode } from "react";

type DialogType = "alert" | "confirm" | "error" | "success";

interface DialogState {
  open: boolean;
  type: DialogType;
  title: string;
  message: string;
  details?: string[];
  confirmLabel?: string;
  cancelLabel?: string;
}

interface DialogContextValue {
  alert: (message: string, title?: string) => Promise<void>;
  confirm: (message: string, title?: string) => Promise<boolean>;
  error: (message: string, details?: string[], title?: string) => Promise<void>;
  success: (message: string, title?: string) => Promise<void>;
}

const DialogContext = createContext<DialogContextValue | null>(null);

export function useDialog() {
  const ctx = useContext(DialogContext);
  if (!ctx) throw new Error("useDialog must be used within DialogProvider");
  return ctx;
}

const initial: DialogState = {
  open: false,
  type: "alert",
  title: "",
  message: "",
};

const iconMap: Record<DialogType, ReactNode> = {
  alert: <Info size={20} className="text-info" />,
  confirm: <AlertCircle size={20} className="text-warning" />,
  error: <AlertCircle size={20} className="text-error" />,
  success: <CheckCircle size={20} className="text-success" />,
};

const titleColorMap: Record<DialogType, string> = {
  alert: "",
  confirm: "text-warning",
  error: "text-error",
  success: "text-success",
};

export function DialogProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<DialogState>(initial);
  const resolveRef = useRef<((value: boolean) => void) | null>(null);

  const show = useCallback((type: DialogType, message: string, title?: string, details?: string[]): Promise<boolean> => {
    return new Promise((resolve) => {
      resolveRef.current = resolve;
      setState({
        open: true,
        type,
        title: title || (type === "error" ? "錯誤" : type === "confirm" ? "確認" : type === "success" ? "成功" : "提示"),
        message,
        details,
      });
    });
  }, []);

  const close = useCallback((result: boolean) => {
    setState(initial);
    resolveRef.current?.(result);
    resolveRef.current = null;
  }, []);

  // useMemo (not useCallback) — this is the context *value* itself, not a
  // callback being handed somewhere. useCallback would only memoize the
  // factory function; calling it inline below (`api()`) built a fresh object
  // every render regardless, which invalidated useDialog() for every
  // consumer app-wide on every DialogProvider render — cascading into
  // unstable useCallback/useMemo deps downstream (e.g. AG Grid columnDefs
  // rebuilding and cell renderers/icons remounting visibly).
  const api = useMemo((): DialogContextValue => ({
    alert: (message, title) => show("alert", message, title).then(() => {}),
    confirm: (message, title) => show("confirm", message, title),
    error: (message, details, title) => show("error", message, title, details).then(() => {}),
    success: (message, title) => show("success", message, title).then(() => {}),
  }), [show]);

  return (
    <DialogContext.Provider value={api}>
      {children}
      <dialog className={`modal ${state.open ? "modal-open" : ""}`}>
        <div className="modal-box">
          <h3 className={`font-bold text-lg flex items-center gap-2 ${titleColorMap[state.type]}`}>
            {iconMap[state.type]}
            {state.title}
          </h3>
          <p className="mt-3 whitespace-pre-wrap">{state.message}</p>
          {state.details && state.details.length > 0 && (
            <ul className="mt-3 space-y-1">
              {state.details.map((d, i) => (
                <li key={i} className="flex items-center gap-2 text-sm p-2 bg-base-200 rounded">
                  <AlertCircle size={14} className="text-error flex-shrink-0" />
                  {d}
                </li>
              ))}
            </ul>
          )}
          <div className="modal-action">
            {state.type === "confirm" ? (
              <>
                <button className="btn btn-sm" onClick={() => close(false)}>取消</button>
                <button className="btn btn-warning btn-sm" onClick={() => close(true)}>確認</button>
              </>
            ) : (
              <button className="btn btn-sm" onClick={() => close(false)}>關閉</button>
            )}
          </div>
        </div>
        <form method="dialog" className="modal-backdrop">
          <button onClick={() => close(false)}>close</button>
        </form>
      </dialog>
    </DialogContext.Provider>
  );
}
