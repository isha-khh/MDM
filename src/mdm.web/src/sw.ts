/// <reference lib="webworker" />
declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ url: string; revision: string | null }>;
};

import { precacheAndRoute } from "workbox-precaching";
import { registerRoute } from "workbox-routing";
import { NetworkFirst } from "workbox-strategies";
import { clientsClaim } from "workbox-core";

// Precaches the app shell (see vite.config.ts's injectManifest.globPatterns
// for what's included) so the /m/* mobile self-service pages can boot with
// no network at all, as long as the device has opened the app at least once
// while online.
precacheAndRoute(self.__WB_MANIFEST);

// NetworkFirst (not CacheFirst/StaleWhileRevalidate): always prefer live
// data when there's a connection, and only fall back to whatever was last
// seen once the network genuinely fails — this is what lets a borrower open
// "今日回報"/"我要歸還" offline and still see their own rental list and the
// category's checklist template, as long as they (or anyone on that device)
// loaded this app while connected at some point before going offline.
registerRoute(
  ({ url, request }) => request.method === "GET" && url.pathname === "/api/rentals",
  new NetworkFirst({ cacheName: "mdm-rentals", networkTimeoutSeconds: 4 }),
);
registerRoute(
  ({ url, request }) => request.method === "GET" && url.pathname === "/api/checklist-templates/resolve",
  new NetworkFirst({ cacheName: "mdm-checklist-templates", networkTimeoutSeconds: 4 }),
);

self.skipWaiting();
clientsClaim();
