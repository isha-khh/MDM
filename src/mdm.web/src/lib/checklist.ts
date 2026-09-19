// Dynamic checklist item, resolved from the category-bound templates
// maintained in Categories.tsx (Phase 2a). Mirrors the backend's
// domain.ChecklistItem. Shared by the desktop return/daily-report dialogs
// (Rentals.tsx) and the mobile self-service pages.
export type ChecklistItemType = "boolean" | "text" | "number" | "location" | "photo";

export interface ChecklistItem {
  key: string;
  label: string;
  type: ChecklistItemType;
  required: boolean;
  unit?: string;
  maxCount?: number;
}

export type ChecklistAnswers = Record<string, unknown>;

export function isChecklistItemFilled(item: ChecklistItem, value: unknown): boolean {
  if (!item.required) return true;
  switch (item.type) {
    case "boolean": return value === true;
    case "photo": return Array.isArray(value) && value.length > 0;
    case "location": return !!value && typeof value === "object";
    case "number": return value !== undefined && value !== null && value !== "";
    case "text": default: return typeof value === "string" && value.trim() !== "";
  }
}

// Downscales an image client-side before upload (long edge capped at
// maxDim, re-encoded as JPEG) — a phone camera photo straight off the
// sensor can be 5-10MB, which would otherwise land directly in the
// database via checklist_photos. Falls back to the original file if
// anything about the canvas path fails.
export function resizeImage(file: File, maxDim: number): Promise<Blob> {
  return new Promise((resolve) => {
    const img = new Image();
    const url = URL.createObjectURL(file);
    img.onload = () => {
      URL.revokeObjectURL(url);
      let { width, height } = img;
      if (width > maxDim || height > maxDim) {
        const scale = maxDim / Math.max(width, height);
        width = Math.round(width * scale);
        height = Math.round(height * scale);
      }
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (!ctx) { resolve(file); return; }
      ctx.drawImage(img, 0, 0, width, height);
      canvas.toBlob((blob) => resolve(blob || file), "image/jpeg", 0.85);
    };
    img.onerror = () => { URL.revokeObjectURL(url); resolve(file); };
    img.src = url;
  });
}
