import { create } from "zustand";
import apiClient from "../lib/apiClient";

export interface DeviceRow {
  udid: string;
  serial_number: string;
  device_name: string;
  asset_number: string;
  model: string;
  os_version: string;
  last_seen: string;
  enrollment_status: string;
  is_supervised: boolean;
  is_lost_mode: boolean;
  battery_level: number;
  custodian_name: string;
  current_holder_name: string;
  category_name: string;
  category_id: string | null;
  custodian_id: string | null;
  asset_status: string;
}

interface DeviceFilters {
  search: string;
  categoryId: string;
  custodianId: string;
}

interface DeviceStore {
  devices: DeviceRow[];
  total: number;
  loading: boolean;
  filters: DeviceFilters;
  selected: Set<string>;

  setFilter: (key: keyof DeviceFilters, value: string) => void;
  clearFilters: () => void;
  loadDevices: () => Promise<void>;
  toggleSelect: (udid: string) => void;
  selectAll: () => void;
  clearSelection: () => void;
  setSelected: (udids: string[]) => void;
}

// Module-level (not store state) since they're plumbing for loadDevices'
// scheduling, not UI state anything should render off of.
let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null;
let requestSeq = 0;

export const useDeviceStore = create<DeviceStore>((set, get) => ({
  devices: [],
  total: 0,
  loading: false,
  filters: { search: "", categoryId: "", custodianId: "" },
  selected: new Set<string>(),

  setFilter: (key, value) => {
    set((s) => ({ filters: { ...s.filters, [key]: value } }));
    if (key === "search") {
      // Free-text search fires on every keystroke — without debouncing this
      // was issuing a full /api/devices-list round trip (and a full grid
      // rowData replacement) per character typed, which is exactly what
      // reads as "AG Grid stutters/lags" while typing in the search box.
      // The dropdown filters (category/custodian) change far less often, so
      // they still apply immediately.
      if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
      searchDebounceTimer = setTimeout(() => get().loadDevices(), 300);
    } else {
      get().loadDevices();
    }
  },

  clearFilters: () => {
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer);
    set({ filters: { search: "", categoryId: "", custodianId: "" } });
    get().loadDevices();
  },

  loadDevices: async () => {
    const seq = ++requestSeq;
    set({ loading: true });
    try {
      const { filters } = get();
      const params: Record<string, string> = {};
      if (filters.search) params.filter = filters.search;
      if (filters.categoryId) params.category_id = filters.categoryId;
      if (filters.custodianId) params.custodian_id = filters.custodianId;
      const { data } = await apiClient.get("/api/devices-list", { params });
      // A slower response for an older keystroke/filter can land after a
      // faster response for a newer one — without this guard the grid could
      // flicker back to stale results for whatever was typed a moment ago.
      if (seq !== requestSeq) return;
      set({ devices: data.devices || [], total: data.total || 0 });
    } catch (err) {
      console.error("Load devices:", err);
    } finally {
      if (seq === requestSeq) set({ loading: false });
    }
  },

  toggleSelect: (udid) => set((s) => {
    const next = new Set(s.selected);
    if (next.has(udid)) next.delete(udid); else next.add(udid);
    return { selected: next };
  }),

  selectAll: () => set((s) => ({
    selected: s.selected.size === s.devices.length
      ? new Set<string>()
      : new Set(s.devices.map((d) => d.udid)),
  })),

  clearSelection: () => set({ selected: new Set<string>() }),
  setSelected: (udids) => set({ selected: new Set<string>(udids) }),
}));
