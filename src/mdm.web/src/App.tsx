import { BrowserRouter, Routes, Route, Navigate } from "react-router";
import { AgGridProvider } from "ag-grid-react";
import { AllEnterpriseModule, IntegratedChartsModule } from "ag-grid-enterprise";

import { useAuthStore } from "./stores/authStore";
import { Layout } from "./components/Layout";
import { ModuleGuard } from "./components/ModuleGuard";
import { ToastContainer } from "./components/ToastContainer";
import { DialogProvider } from "./components/DialogProvider";
import { Login } from "./pages/Login";
import { Setup } from "./pages/Setup";
import { Dashboard } from "./pages/Dashboard";
import { Devices } from "./pages/Devices";
import { DeviceDetail } from "./pages/DeviceDetail";
import { Commands } from "./pages/Commands";
import { Apps } from "./pages/Apps";
import { Profiles } from "./pages/Profiles";
import { Events } from "./pages/Events";
import { Users } from "./pages/Users";
import { Audit } from "./pages/Audit";
import { Rentals } from "./pages/Rentals";
import { MaintenanceRequests } from "./pages/MaintenanceRequests";
import { DisposalRequests } from "./pages/DisposalRequests";
import { Categories } from "./pages/Categories";
import { Inventory } from "./pages/Inventory";
import { AssetList } from "./pages/AssetList";
import { Notifications } from "./pages/Notifications";
import { Settings } from "./pages/Settings";
import { DEPTemplates } from "./pages/DEPTemplates";
import { useState, useEffect, Suspense, lazy } from "react";
import { useParams } from "react-router";
import { wireAutoFlush } from "./lib/offlineQueue";

// Lazy-loaded so the mobile self-service bundle (and its PWA-cached app
// shell) stays a small, separate chunk from the desktop admin app — a phone
// only ever needs this piece offline, never the AG Grid-heavy desktop pages.
const MobileHome = lazy(() => import("./pages/mobile/MobileHome").then((m) => ({ default: m.MobileHome })));
const MobileDailyReport = lazy(() => import("./pages/mobile/MobileDailyReport").then((m) => ({ default: m.MobileDailyReport })));
const MobileSubmitReturn = lazy(() => import("./pages/mobile/MobileSubmitReturn").then((m) => ({ default: m.MobileSubmitReturn })));

function MobileFallback() {
  return <div className="min-h-screen flex items-center justify-center bg-base-200"><span className="loading loading-spinner loading-lg text-primary"></span></div>;
}

function DeviceRedirect() {
  const { udid } = useParams();
  return <Navigate to={`/mdm/devices/${udid}`} replace />;
}
import apiClient from "./lib/apiClient";
import {AgChartsEnterpriseModule} from "ag-charts-enterprise";

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading } = useAuthStore();
  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-base-200">
        <span className="loading loading-spinner loading-lg text-primary"></span>
      </div>
    );
  }
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

function AppRoutes() {
  const { isAuthenticated, isLoading, checkAuth } = useAuthStore();
  const [initialized, setInitialized] = useState<boolean | null>(null);

  useEffect(() => { checkAuth(); }, [checkAuth]);

  useEffect(() => {
    apiClient.get("/api/system-status")
      .then(({ data }) => setInitialized(data.initialized))
      .catch(() => setInitialized(true));
  }, []);

  // Retries the mobile self-service pages' offline queue on reconnect/app
  // foreground regardless of which route is active — an installed PWA can
  // reopen straight onto /m/rentals, but a desktop session left mid-sync
  // should still catch up too.
  useEffect(() => { if (isAuthenticated) wireAutoFlush(); }, [isAuthenticated]);

  if (initialized === null || isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-base-200">
        <span className="loading loading-spinner loading-lg text-primary"></span>
      </div>
    );
  }

  if (!initialized) {
    return (
      <Routes>
        <Route path="/setup" element={<Setup />} />
        <Route path="*" element={<Navigate to="/setup" replace />} />
      </Routes>
    );
  }

  return (
    <Routes>
      <Route path="/setup" element={<Navigate to="/login" replace />} />
      <Route path="/login" element={isAuthenticated ? <Navigate to="/dashboard" /> : <Login />} />

      {/* Mobile self-service (借用人手機版): no sidebar/Layout, own minimal
          chrome (MobileShell) — reached from a home-screen PWA install, so
          it deliberately doesn't share the desktop admin app's routes. */}
      <Route
        path="/m/rentals"
        element={
          <ProtectedRoute>
            <ModuleGuard module="rental" minLevel="requester">
              <Suspense fallback={<MobileFallback />}><MobileHome /></Suspense>
            </ModuleGuard>
          </ProtectedRoute>
        }
      />
      <Route
        path="/m/rentals/:rentalId/daily-report"
        element={
          <ProtectedRoute>
            <ModuleGuard module="rental" minLevel="requester">
              <Suspense fallback={<MobileFallback />}><MobileDailyReport /></Suspense>
            </ModuleGuard>
          </ProtectedRoute>
        }
      />
      <Route
        path="/m/rentals/:rentalId/submit-return"
        element={
          <ProtectedRoute>
            <ModuleGuard module="rental" minLevel="requester">
              <Suspense fallback={<MobileFallback />}><MobileSubmitReturn /></Suspense>
            </ModuleGuard>
          </ProtectedRoute>
        }
      />
      <Route
        element={
          <ProtectedRoute>
            <Layout />
            <ToastContainer />
          </ProtectedRoute>
        }
      >
        <Route path="/dashboard" element={<Dashboard />} />

        {/* Asset module */}
        <Route path="/asset/list" element={<ModuleGuard module="asset"><AssetList /></ModuleGuard>} />
        <Route path="/asset/categories" element={<ModuleGuard module="asset"><Categories /></ModuleGuard>} />
        <Route path="/asset/inventory" element={<ModuleGuard module="asset" minLevel="operator"><Inventory /></ModuleGuard>} />

        {/* MDM module */}
        <Route path="/mdm/devices" element={<ModuleGuard module="mdm"><Devices /></ModuleGuard>} />
        <Route path="/mdm/devices/:udid" element={<ModuleGuard module="mdm"><DeviceDetail /></ModuleGuard>} />
        <Route path="/mdm/commands" element={<ModuleGuard module="mdm" minLevel="operator"><Commands /></ModuleGuard>} />
        <Route path="/mdm/apps" element={<ModuleGuard module="mdm" minLevel="operator"><Apps /></ModuleGuard>} />
        <Route path="/mdm/profiles" element={<ModuleGuard module="mdm" minLevel="operator"><Profiles /></ModuleGuard>} />
        <Route path="/mdm/events" element={<ModuleGuard module="mdm"><Events /></ModuleGuard>} />
        <Route path="/mdm/dep-templates" element={<ModuleGuard module="mdm" minLevel="operator"><DEPTemplates /></ModuleGuard>} />

        {/* Rental module */}
        <Route path="/rental/list" element={<ModuleGuard module="rental" minLevel="requester"><Rentals /></ModuleGuard>} />
        <Route path="/rental/notifications" element={<ModuleGuard module="rental" minLevel="approver"><Notifications /></ModuleGuard>} />

        {/* Maintenance module */}
        <Route path="/maintenance/list" element={<ModuleGuard module="maintenance" minLevel="requester"><MaintenanceRequests /></ModuleGuard>} />

        {/* Disposal module */}
        <Route path="/disposal/list" element={<ModuleGuard module="disposal" minLevel="requester"><DisposalRequests /></ModuleGuard>} />

        {/* Admin */}
        <Route path="/admin/users" element={<Users />} />
        <Route path="/admin/audit" element={<Audit />} />
        <Route path="/admin/settings" element={<Settings />} />

        {/* Backward-compatible redirects */}
        <Route path="/devices/:udid" element={<DeviceRedirect />} />
        <Route path="/devices" element={<Navigate to="/mdm/devices" replace />} />
        <Route path="/commands" element={<Navigate to="/mdm/commands" replace />} />
        <Route path="/apps" element={<Navigate to="/mdm/apps" replace />} />
        <Route path="/profiles" element={<Navigate to="/mdm/profiles" replace />} />
        <Route path="/events" element={<Navigate to="/mdm/events" replace />} />
        <Route path="/rentals" element={<Navigate to="/rental/list" replace />} />
        <Route path="/categories" element={<Navigate to="/asset/categories" replace />} />
        <Route path="/users" element={<Navigate to="/admin/users" replace />} />
        <Route path="/audit" element={<Navigate to="/admin/audit" replace />} />

        <Route path="/" element={<Navigate to="/dashboard" replace />} />
      </Route>
    </Routes>
  );
}

const agGridModules = [
  AllEnterpriseModule,
  IntegratedChartsModule.with(AgChartsEnterpriseModule),
];

export default function App() {
  return (
    <AgGridProvider
      modules={agGridModules}
      licenseKey={import.meta.env.VITE_AG_GRID_LICENSE as string | undefined}
    >
      <BrowserRouter>
        <DialogProvider>
          <AppRoutes />
        </DialogProvider>
      </BrowserRouter>
    </AgGridProvider>
  );
}
