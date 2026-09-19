import { useSyncExternalStore } from "react";
import axios from "axios";
import { openDB, type DBSchema, type IDBPDatabase } from "idb";
import apiClient from "./apiClient";
import type { ChecklistAnswers } from "./checklist";

// Offline-first submission queue for the mobile self-service pages
// (src/pages/mobile/*): 每日回報/送出歸還 filled out without a VPN
// connection are staged here (IndexedDB, survives app restarts) instead of
// failing outright, and get sent for real once connectivity — including
// getting back on the company VPN — returns. The desktop Rentals.tsx flows
// are unaffected; they always require a live connection, same as before.

export type PendingActionType = "daily-report" | "submit-return";

export interface PendingAction {
  id: string;
  type: PendingActionType;
  rentalId: string;
  rentalNumber: number;
  assetLabel: string;
  body: Record<string, unknown>; // may contain "local:<uuid>" photo placeholders inside body.checklist
  createdAt: number;
  status: "pending" | "failed";
  error?: string;
}

interface PhotoBlobRecord {
  tempId: string;
  blob: Blob;
}

interface QueueDB extends DBSchema {
  actions: { key: string; value: PendingAction };
  photos: { key: string; value: PhotoBlobRecord };
}

let dbPromise: Promise<IDBPDatabase<QueueDB>> | null = null;
function getDB() {
  if (!dbPromise) {
    dbPromise = openDB<QueueDB>("mdm-offline-queue", 1, {
      upgrade(db) {
        db.createObjectStore("actions", { keyPath: "id" });
        db.createObjectStore("photos", { keyPath: "tempId" });
      },
    });
  }
  return dbPromise;
}

export function isNetworkError(err: unknown): boolean {
  return axios.isAxiosError(err) && !err.response;
}

// ---- local photo staging (composing a form while offline) ----

const localPreviewUrls = new Map<string, string>();

export async function stagePhotoLocally(blob: Blob): Promise<string> {
  const tempId = `local:${crypto.randomUUID()}`;
  const db = await getDB();
  await db.put("photos", { tempId, blob });
  localPreviewUrls.set(tempId, URL.createObjectURL(blob));
  return tempId;
}

export function getLocalPhotoPreview(id: string): string | undefined {
  return localPreviewUrls.get(id);
}

// For the mobile self-service pages' ChecklistFields: upload for real when
// online, stage locally (to be resolved at flush time) when offline or when
// the upload itself fails for network reasons. A non-network failure (e.g.
// file rejected) is rethrown so the field's own error handling still runs.
export async function uploadOrStagePhoto(file: File, rentalNumber: number, itemKey: string): Promise<string> {
  if (navigator.onLine) {
    try {
      const form = new FormData();
      form.append("file", file, file.name);
      form.append("rental_number", String(rentalNumber));
      form.append("item_key", itemKey);
      const { data } = await apiClient.post("/api/checklist-photos", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      return data.id;
    } catch (err) {
      if (!isNetworkError(err)) throw err;
    }
  }
  return stagePhotoLocally(file);
}

export function resolvePhotoSrc(id: string): string {
  if (id.startsWith("local:")) return getLocalPhotoPreview(id) || "";
  return `/api/checklist-photos/${id}`;
}

export function hasStagedPhotos(checklist: ChecklistAnswers): boolean {
  return Object.values(checklist).some((v) => Array.isArray(v) && v.some((x) => typeof x === "string" && x.startsWith("local:")));
}

async function discardLocalPhoto(id: string) {
  const db = await getDB();
  await db.delete("photos", id);
  const preview = localPreviewUrls.get(id);
  if (preview) { URL.revokeObjectURL(preview); localPreviewUrls.delete(id); }
}

// ---- queue read/write + a React-friendly in-memory mirror ----

let cachedActions: PendingAction[] = [];
const listeners = new Set<() => void>();
function notify() { listeners.forEach((l) => l()); }

async function refreshCache() {
  const db = await getDB();
  cachedActions = (await db.getAll("actions")).sort((a, b) => a.createdAt - b.createdAt);
  notify();
}
void refreshCache();

export function usePendingActions(): PendingAction[] {
  return useSyncExternalStore(
    (cb) => { listeners.add(cb); return () => listeners.delete(cb); },
    () => cachedActions,
    () => cachedActions,
  );
}

export function useOnlineStatus(): boolean {
  return useSyncExternalStore(
    (cb) => {
      window.addEventListener("online", cb);
      window.addEventListener("offline", cb);
      return () => { window.removeEventListener("online", cb); window.removeEventListener("offline", cb); };
    },
    () => navigator.onLine,
    () => true,
  );
}

export async function enqueueAction(action: Omit<PendingAction, "id" | "status" | "createdAt" | "error">): Promise<void> {
  const db = await getDB();
  await db.put("actions", { ...action, id: crypto.randomUUID(), status: "pending", createdAt: Date.now() });
  await refreshCache();
}

export async function discardAction(id: string): Promise<void> {
  const db = await getDB();
  const action = await db.get("actions", id);
  const ids = extractPhotoIds(action?.body.checklist);
  for (const photoId of ids) if (photoId.startsWith("local:")) await discardLocalPhoto(photoId);
  await db.delete("actions", id);
  await refreshCache();
}

function extractPhotoIds(checklist: unknown): string[] {
  if (!checklist || typeof checklist !== "object") return [];
  return Object.values(checklist as ChecklistAnswers)
    .filter((v): v is string[] => Array.isArray(v) && v.every((x) => typeof x === "string"))
    .flat();
}

// Uploads any "local:<uuid>" staged photos referenced in a checklist answer,
// swapping each for the real checklist_photos id the server hands back.
async function resolveChecklistPhotos(checklist: ChecklistAnswers, rentalNumber: number): Promise<ChecklistAnswers> {
  const db = await getDB();
  const resolved: ChecklistAnswers = { ...checklist };
  for (const [key, value] of Object.entries(checklist)) {
    if (!Array.isArray(value) || !value.every((v) => typeof v === "string")) continue;
    const ids = value as string[];
    if (!ids.some((id) => id.startsWith("local:"))) continue;
    const newIds: string[] = [];
    for (const id of ids) {
      if (!id.startsWith("local:")) { newIds.push(id); continue; }
      const rec = await db.get("photos", id);
      if (!rec) continue; // already resolved/discarded — skip rather than crash the sync
      const form = new FormData();
      form.append("file", rec.blob, "photo.jpg");
      form.append("rental_number", String(rentalNumber));
      form.append("item_key", key);
      const { data } = await apiClient.post("/api/checklist-photos", form, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      newIds.push(data.id);
      await discardLocalPhoto(id);
    }
    resolved[key] = newIds;
  }
  return resolved;
}

const ENDPOINTS: Record<PendingActionType, (rentalId: string) => string> = {
  "daily-report": (id) => `/api/rentals/${id}/daily-report`,
  "submit-return": (id) => `/api/rentals/${id}/submit-return`,
};

let flushing = false;

// Sends every queued action in creation order. Stops at the first network
// failure (further attempts right now would just fail the same way) but
// keeps going past a real server-side rejection (e.g. "already reported
// today") so one bad item doesn't block the rest of the queue behind it.
export async function flush(): Promise<void> {
  if (flushing || !navigator.onLine) return;
  flushing = true;
  try {
    const db = await getDB();
    const actions = (await db.getAll("actions")).sort((a, b) => a.createdAt - b.createdAt);
    for (const action of actions) {
      try {
        const body = { ...action.body };
        if (body.checklist) {
          body.checklist = await resolveChecklistPhotos(body.checklist as ChecklistAnswers, action.rentalNumber);
        }
        await apiClient.post(ENDPOINTS[action.type](action.rentalId), body);
        await db.delete("actions", action.id);
      } catch (err) {
        if (isNetworkError(err)) break;
        const resp = (err as { response?: { data?: { error?: string } } })?.response?.data;
        await db.put("actions", { ...action, status: "failed", error: resp?.error || "同步失敗，請檢查後手動重試" });
      }
    }
  } finally {
    flushing = false;
    await refreshCache();
  }
}

let wired = false;
export function wireAutoFlush(): void {
  if (wired) return;
  wired = true;
  window.addEventListener("online", () => { void flush(); });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && navigator.onLine) void flush();
  });
  void flush();
}
