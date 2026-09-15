import { create } from "zustand";
import { parsePlist, base64ToUtf8 } from "../lib/plist";
import apiClient from "../lib/apiClient";
import type { MDMEvent } from "../gen/mdm/v1/event_pb";

export interface TrackedCommand {
  id: string;
  label: string;
  udids: string[];
  sentAt: Date;
  status: "sent" | "acknowledged" | "error";
  commandUuid?: string;
  responseAt?: Date;
}

export interface Toast {
  id: string;
  type: "success" | "error" | "info";
  message: string;
  timestamp: Date;
}

let toastCounter = 0;
let cmdCounter = 0;

interface EventStore {
  streaming: boolean;
  events: MDMEvent[];
  trackedCommands: TrackedCommand[];
  toasts: Toast[];
  unreadCount: number;

  setStreaming: (v: boolean) => void;
  clearEvents: () => void;
  processEvent: (event: MDMEvent) => void;
  trackCommand: (label: string, udids: string[], commandUuid?: string) => string;
  dismissCommand: (id: string) => void;
  clearCompletedCommands: () => void;
  addToast: (type: Toast["type"], message: string) => void;
  dismissToast: (id: string) => void;
  markAllRead: () => void;
}

export const useEventStore = create<EventStore>((set, get) => ({
  streaming: true,
  events: [],
  trackedCommands: [],
  toasts: [],
  unreadCount: 0,

  setStreaming: (v) => set({ streaming: v }),
  clearEvents: () => set({ events: [] }),
  markAllRead: () => set({ unreadCount: 0 }),

  addToast: (type, message) => {
    const id = `toast-${++toastCounter}`;
    set((s) => ({ toasts: [...s.toasts, { id, type, message, timestamp: new Date() }] }));
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), 5000);
  },

  dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),

  trackCommand: (label, udids, commandUuid) => {
    const id = `cmd-${++cmdCounter}`;
    set((s) => ({
      trackedCommands: [{ id, label, udids, sentAt: new Date(), status: "sent" as const, commandUuid }, ...s.trackedCommands].slice(0, 50),
    }));
    get().addToast("info", `${label} → ${udids.length} device(s)`);
    return id;
  },

  dismissCommand: (id) => set((s) => ({
    trackedCommands: s.trackedCommands.filter((c) => c.id !== id),
  })),

  clearCompletedCommands: () => set((s) => ({
    trackedCommands: s.trackedCommands.filter((c) => c.status === "sent"),
  })),

  processEvent: (event) => {
    const { addToast } = get();

    // Keep a rolling event log, but do NOT bump unreadCount here — the bell
    // badge must only count things that will actually show up when the user
    // opens the tracker. CommandTracker renders trackedCommands exclusively
    // (not this raw event log), so counting every incoming event here
    // (including background device check-ins that have nothing to do with
    // anything the user sent) left the badge climbing while the panel opened
    // empty. unreadCount is now only incremented in the branches below,
    // and only when they actually update a tracked command.
    set((s) => ({ events: [event, ...s.events].slice(0, 200) }));

    const status = event.status?.toLowerCase();
    const eventType = event.eventType?.toLowerCase();

    // command_sent
    if (eventType === "command_sent" && event.commandUuid) {
      set((s) => {
        let matched = false;
        const trackedCommands = s.trackedCommands.map((cmd) => {
          if (!matched && cmd.commandUuid === event.commandUuid) {
            matched = true;
            return { ...cmd, status: "acknowledged" as const, responseAt: new Date() };
          }
          return cmd;
        });
        return { trackedCommands, unreadCount: matched ? s.unreadCount + 1 : s.unreadCount };
      });
      addToast("success", `${event.udid?.slice(0, 12)}... — sent`);
      return;
    }

    // command_error
    if (eventType === "command_error" && event.commandUuid) {
      set((s) => {
        let matched = false;
        const trackedCommands = s.trackedCommands.map((cmd) => {
          if (cmd.commandUuid === event.commandUuid) {
            matched = true;
            return { ...cmd, status: "error" as const, responseAt: new Date() };
          }
          return cmd;
        });
        return { trackedCommands, unreadCount: matched ? s.unreadCount + 1 : s.unreadCount };
      });
      addToast("error", `${event.udid?.slice(0, 12)}... — error`);
      return;
    }

    // acknowledge from MicroMDM
    if (eventType === "acknowledge" && event.commandUuid) {
      const newStatus = status === "error" ? "error" as const : "acknowledged" as const;
      set((s) => {
        let matched = false;
        const trackedCommands = s.trackedCommands.map((cmd) => {
          if (!matched && cmd.commandUuid === event.commandUuid) {
            matched = true;
            addToast(newStatus === "acknowledged" ? "success" : "error", `${cmd.label} — ${event.status}`);
            return { ...cmd, status: newStatus, responseAt: new Date() };
          }
          return cmd;
        });
        return { trackedCommands, unreadCount: matched ? s.unreadCount + 1 : s.unreadCount };
      });

      // Auto-save DeviceInformation to DB
      if (newStatus === "acknowledged" && event.rawPayload && event.udid) {
        saveDeviceInfo(event.udid, event.rawPayload);
      }
      return;
    }

    if (eventType === "acknowledge" && status === "idle") return;
    if (eventType === "checkin") return;

    addToast("info", `${event.eventType} — ${event.udid?.slice(0, 12) || "system"}...`);
  },
}));

function saveDeviceInfo(udid: string, rawPayload: string) {
  try {
    const xml = base64ToUtf8(rawPayload);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const parsed = parsePlist(xml) as any;
    if (!parsed?.QueryResponses) return;
    const qr = parsed.QueryResponses;
    const body: Record<string, unknown> = {};
    if (qr.DeviceName) body.device_name = qr.DeviceName;
    if (qr.ModelName || qr.Model) body.model = qr.ModelName || qr.Model;
    if (qr.OSVersion) body.os_version = qr.OSVersion;
    if (qr.SerialNumber) body.serial_number = qr.SerialNumber;
    if (Object.keys(body).length === 0) return;
    apiClient.put(`/api/devices/${udid}`, body).catch(() => {});
  } catch { /* skip */ }
}
