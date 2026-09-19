import { useState } from "react";
import { MapPin, Camera, X } from "lucide-react";
import { type ChecklistItem, type ChecklistAnswers, resizeImage } from "../lib/checklist";

export function ChecklistLocationField({ item, value, onChange }: {
  item: ChecklistItem;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const [manual, setManual] = useState(false);
  const [busy, setBusy] = useState(false);
  const v = value as { lat?: number; lng?: number; address?: string } | undefined;

  const capture = () => {
    setBusy(true);
    if (!navigator.geolocation) { setManual(true); setBusy(false); return; }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        onChange({ lat: pos.coords.latitude, lng: pos.coords.longitude, accuracy: pos.coords.accuracy, capturedAt: new Date().toISOString() });
        setBusy(false);
      },
      () => { setManual(true); setBusy(false); },
      { enableHighAccuracy: true, timeout: 10000 },
    );
  };

  return (
    <div className="form-control">
      <label className="label">
        <span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
      </label>
      {v?.lat != null ? (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-sm flex-wrap">
            <MapPin size={14} className="text-success" />
            <a className="link link-primary" target="_blank" rel="noreferrer" href={`https://maps.google.com/?q=${v.lat},${v.lng}`}>
              已定位（開啟地圖）
            </a>
            <button type="button" className="btn btn-ghost btn-xs" onClick={capture}>重新定位</button>
          </div>
          {/* Simple embed, no API key needed (Google's legacy "output=embed"
              parameter) — just a quick visual preview, "開啟地圖" above is
              still the way to get directions/a full interactive map. Works
              offline too as far as the capture itself (device GPS); the
              preview iframe just won't load until back online. */}
          <iframe
            title={`${item.label} 地圖預覽`}
            src={`https://maps.google.com/maps?q=${v.lat},${v.lng}&z=16&output=embed`}
            className="w-full h-40 rounded border border-base-300"
            loading="lazy"
          />
        </div>
      ) : manual ? (
        <input
          type="text"
          className="input input-bordered input-sm"
          placeholder="手動輸入地址"
          value={v?.address || ""}
          onChange={(e) => onChange({ address: e.target.value })}
        />
      ) : (
        <div className="flex items-center gap-2">
          <button type="button" className="btn btn-outline btn-sm gap-1" disabled={busy} onClick={capture}>
            {busy ? <span className="loading loading-spinner loading-xs" /> : <MapPin size={14} />} 取得目前位置
          </button>
          <button type="button" className="btn btn-link btn-xs" onClick={() => setManual(true)}>改用手動輸入地址</button>
        </div>
      )}
    </div>
  );
}

export function ChecklistPhotoField({ item, rentalNumber, value, onChange, onUpload, resolveSrc }: {
  item: ChecklistItem;
  rentalNumber: number;
  value: unknown;
  onChange: (v: string[]) => void;
  /** Uploads (or, offline, stages) one photo and returns the id to store in
   * the checklist answer. Desktop uploads immediately; the mobile
   * self-service pages stage a local blob and return a "local:<uuid>"
   * placeholder when offline (see src/lib/offlineQueue.ts). */
  onUpload: (file: File, item: ChecklistItem, rentalNumber: number) => Promise<string>;
  /** Resolves an id to an <img> src. Defaults to the real download endpoint;
   * mobile overrides this to serve a local object URL for staged-offline
   * ("local:...") ids. */
  resolveSrc?: (id: string) => string;
}) {
  const [uploading, setUploading] = useState(false);
  const ids = Array.isArray(value) ? (value as string[]) : [];
  const max = item.maxCount || 0;

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setUploading(true);
    try {
      const resized = await resizeImage(file, 1600);
      const resizedFile = new File([resized], file.name || "photo.jpg", { type: "image/jpeg" });
      const id = await onUpload(resizedFile, item, rentalNumber);
      onChange([...ids, id]);
    } catch { /* best-effort — user can just try uploading again */ }
    finally { setUploading(false); }
  };

  const src = (id: string) => (resolveSrc ? resolveSrc(id) : `/api/checklist-photos/${id}`);

  return (
    <div className="form-control">
      <label className="label">
        <span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
      </label>
      <div className="flex flex-wrap gap-2 items-center">
        {ids.map((id) => (
          <div key={id} className="relative">
            <img src={src(id)} alt="" className="w-16 h-16 object-cover rounded border border-base-300" />
            <button
              type="button"
              className="btn btn-error btn-xs btn-circle absolute -top-2 -right-2"
              onClick={() => onChange(ids.filter((existing) => existing !== id))}
            >
              <X size={10} />
            </button>
          </div>
        ))}
        {(max === 0 || ids.length < max) && (
          <label className="btn btn-outline btn-sm gap-1 cursor-pointer">
            {uploading ? <span className="loading loading-spinner loading-xs" /> : <Camera size={14} />} 拍照/上傳
            <input type="file" accept="image/*" capture="environment" className="hidden" onChange={handleFile} disabled={uploading} />
          </label>
        )}
      </div>
    </div>
  );
}

// Shared dynamic checklist renderer — used by the desktop return/daily-report
// dialogs (Rentals.tsx) and the mobile self-service pages, so a category's
// checklist template renders identically everywhere it's answered.
export function ChecklistFields({ items, values, onChange, rentalNumber, onUploadPhoto, resolvePhotoSrc }: {
  items: ChecklistItem[];
  values: ChecklistAnswers;
  onChange: (key: string, value: unknown) => void;
  rentalNumber: number;
  onUploadPhoto: (file: File, item: ChecklistItem, rentalNumber: number) => Promise<string>;
  resolvePhotoSrc?: (id: string) => string;
}) {
  return (
    <div className="space-y-3">
      {items.map((item) => {
        const value = values[item.key];
        const setValue = (v: unknown) => onChange(item.key, v);
        if (item.type === "boolean") {
          return (
            <label key={item.key} className="flex items-center gap-3 cursor-pointer p-2 rounded hover:bg-base-200">
              <input
                type="checkbox"
                className="checkbox checkbox-sm checkbox-success"
                checked={value === true}
                onChange={(e) => setValue(e.target.checked)}
              />
              <span className="text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span>
            </label>
          );
        }
        if (item.type === "text") {
          return (
            <div key={item.key} className="form-control">
              <label className="label"><span className="label-text text-sm">{item.label}{item.required && <span className="text-error"> *</span>}</span></label>
              <input type="text" className="input input-bordered input-sm" value={(value as string) || ""} onChange={(e) => setValue(e.target.value)} />
            </div>
          );
        }
        if (item.type === "number") {
          return (
            <div key={item.key} className="form-control">
              <label className="label">
                <span className="label-text text-sm">{item.label}{item.unit ? `（${item.unit}）` : ""}{item.required && <span className="text-error"> *</span>}</span>
              </label>
              <input
                type="number"
                className="input input-bordered input-sm"
                value={value === undefined || value === null ? "" : (value as number)}
                onChange={(e) => setValue(e.target.value === "" ? "" : Number(e.target.value))}
              />
            </div>
          );
        }
        if (item.type === "location") {
          return <ChecklistLocationField key={item.key} item={item} value={value} onChange={setValue} />;
        }
        return (
          <ChecklistPhotoField
            key={item.key}
            item={item}
            rentalNumber={rentalNumber}
            value={value}
            onChange={setValue}
            onUpload={onUploadPhoto}
            resolveSrc={resolvePhotoSrc}
          />
        );
      })}
    </div>
  );
}
