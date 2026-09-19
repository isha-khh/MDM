import { useEffect, useState } from "react";
import apiClient from "../lib/apiClient";
import { type RentalGroup, groupByRentalNumber } from "../lib/rentalTypes";

// Used by the mobile self-service pages (daily-report/submit-return), which
// are reached from the home list but must also survive a page reload — a
// backgrounded PWA tab can be killed by the OS mid-task — so they re-derive
// the batch from a fresh GET /api/rentals rather than relying only on
// react-router navigation state.
export function useRentalGroup(rentalId: string | undefined) {
  const [group, setGroup] = useState<RentalGroup | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);

  useEffect(() => {
    if (!rentalId) return;
    let cancelled = false;
    setLoading(true);
    setLoadError(false);
    apiClient.get("/api/rentals")
      .then(({ data }) => {
        if (cancelled) return;
        const found = groupByRentalNumber(data.rentals || []).find((g) => g.rentals.some((r) => r.id === rentalId));
        setGroup(found || null);
        if (!found) setLoadError(true);
      })
      .catch(() => { if (!cancelled) setLoadError(true); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [rentalId]);

  return { group, loading, loadError };
}
