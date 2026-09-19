import { Link, Outlet, useLocation } from "react-router";
import { useAuthStore } from "../stores/authStore";
import { useEventStore } from "../stores/eventStore";
import { useEventStream } from "../hooks/useEventStream";
import { useTranslation } from "react-i18next";
import { CommandTracker } from "./CommandTracker";
import { ChangePassword } from "./ChangePassword";
import { ViewerOnboarding } from "./ViewerOnboarding";
import {
  LayoutDashboard, Tablet, Terminal, Radio, Users, ClipboardList, FileText, Package, Repeat, FolderTree,
  LogOut, Menu, Moon, Sun, Globe, Bell, Wifi, WifiOff, Lock, Briefcase, BellRing, ClipboardCheck, Settings, ScrollText,
  Wrench, Trash2,
} from "lucide-react";
import { useState, useEffect, type ReactNode } from "react";

interface NavItem {
  path: string;
  labelKey: string;
  icon: ReactNode;
  module?: string;
  minLevel?: string;
  sysAdmin?: boolean;
}

interface NavGroup {
  labelKey: string;
  module?: string;
  sysAdmin?: boolean;
  items: NavItem[];
}

const navGroups: NavGroup[] = [
  {
    labelKey: "",
    items: [
      { path: "/dashboard", labelKey: "nav.dashboard", icon: <LayoutDashboard size={20} /> },
    ],
  },
  {
    labelKey: "nav.group_asset",
    module: "asset",
    items: [
      { path: "/asset/list", labelKey: "nav.assets", icon: <Briefcase size={20} />, module: "asset" },
      { path: "/asset/categories", labelKey: "nav.categories", icon: <FolderTree size={20} />, module: "asset" },
      { path: "/asset/inventory", labelKey: "nav.inventory", icon: <ClipboardCheck size={20} />, module: "asset", minLevel: "operator" },
    ],
  },
  {
    labelKey: "nav.group_mdm",
    module: "mdm",
    items: [
      { path: "/mdm/devices", labelKey: "nav.devices", icon: <Tablet size={20} />, module: "mdm" },
      { path: "/mdm/commands", labelKey: "nav.commands", icon: <Terminal size={20} />, module: "mdm", minLevel: "operator" },
      { path: "/mdm/apps", labelKey: "nav.apps", icon: <Package size={20} />, module: "mdm", minLevel: "operator" },
      { path: "/mdm/profiles", labelKey: "nav.profiles", icon: <FileText size={20} />, module: "mdm", minLevel: "operator" },
      { path: "/mdm/events", labelKey: "nav.events", icon: <Radio size={20} />, module: "mdm" },
      { path: "/mdm/dep-templates", labelKey: "nav.depTemplates", icon: <ScrollText size={20} />, module: "mdm", minLevel: "operator" },
    ],
  },
  {
    labelKey: "nav.group_rental",
    module: "rental",
    items: [
      { path: "/rental/list", labelKey: "nav.rentals", icon: <Repeat size={20} />, module: "rental" },
      { path: "/rental/notifications", labelKey: "nav.notifications", icon: <BellRing size={20} />, module: "rental", minLevel: "approver" },
    ],
  },
  {
    labelKey: "nav.group_maintenance",
    module: "maintenance",
    items: [
      { path: "/maintenance/list", labelKey: "nav.maintenanceRequests", icon: <Wrench size={20} />, module: "maintenance" },
    ],
  },
  {
    labelKey: "nav.group_disposal",
    module: "disposal",
    items: [
      { path: "/disposal/list", labelKey: "nav.disposalRequests", icon: <Trash2 size={20} />, module: "disposal" },
    ],
  },
  {
    labelKey: "nav.group_admin",
    sysAdmin: true,
    items: [
      { path: "/admin/users", labelKey: "nav.users", icon: <Users size={20} />, sysAdmin: true },
      { path: "/admin/audit", labelKey: "nav.audit", icon: <ClipboardList size={20} />, sysAdmin: true },
      { path: "/admin/settings", labelKey: "nav.settings", icon: <Settings size={20} />, sysAdmin: true },
    ],
  },
];

const levelOrder: Record<string, number> = {
  none: 0, viewer: 1, requester: 2, operator: 3, approver: 4, manager: 5,
};

const languages = [
  { code: "zh-TW", label: "繁體中文" },
  { code: "en", label: "English" },
];

function useNavFilter() {
  const { user, modulePermissions } = useAuthStore();
  const isSysAdmin = user?.system_role === "sys_admin" || user?.role === "admin";

  function hasModuleAccess(module?: string, minLevel?: string) {
    if (!module) return true;
    if (isSysAdmin) return true;
    const level = modulePermissions[module] || "none";
    const required = minLevel || "viewer";
    return (levelOrder[level] ?? 0) >= (levelOrder[required] ?? 0);
  }

  const filtered: { label: string; items: NavItem[] }[] = [];
  for (const group of navGroups) {
    if (group.sysAdmin && !isSysAdmin) continue;
    if (group.module && !hasModuleAccess(group.module)) continue;

    const visibleItems = group.items.filter((item) => {
      if (item.sysAdmin && !isSysAdmin) return false;
      return hasModuleAccess(item.module, item.minLevel);
    });

    if (visibleItems.length > 0) {
      filtered.push({ label: group.labelKey, items: visibleItems });
    }
  }
  return filtered;
}

export function Layout() {
  const { user, logout } = useAuthStore();
  // Individual selectors, not a whole-store destructure: Layout wraps every
  // page via <Outlet/>, and the event store's `events` array is appended to
  // on every single incoming MDM event (device check-ins, command acks —
  // arriving continuously and often, independent of user interaction). A
  // bare `useEventStore()` subscribes to the *entire* store, so every one of
  // those events was re-rendering the whole app, including whatever AG Grid
  // happened to be mounted on the current page — the actual cause behind
  // reports of the grid "stuttering" that had nothing to do with that grid
  // itself. Selecting only the specific fields this component reads means
  // Layout only re-renders when one of THOSE fields actually changes.
  const streaming = useEventStore((s) => s.streaming);
  const setStreaming = useEventStore((s) => s.setStreaming);
  const unreadCount = useEventStore((s) => s.unreadCount);
  const markAllRead = useEventStore((s) => s.markAllRead);
  const trackedCommands = useEventStore((s) => s.trackedCommands);
  const { t, i18n } = useTranslation();
  const location = useLocation();
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    (localStorage.getItem("theme") as "light" | "dark") || "light"
  );
  const [showTracker, setShowTracker] = useState(false);
  const [showChangePw, setShowChangePw] = useState(false);

  const filteredGroups = useNavFilter();

  // Start the gRPC event stream
  useEventStream();

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("theme", theme);
  }, [theme]);

  const toggleTheme = () => setTheme(theme === "light" ? "dark" : "light");
  const switchLang = (code: string) => { i18n.changeLanguage(code); localStorage.setItem("lang", code); };

  // Find current page label across all groups
  const allItems = filteredGroups.flatMap((g) => g.items);
  const currentPage = allItems.find((item) => location.pathname.startsWith(item.path));
  const pendingCommands = trackedCommands.filter((c) => c.status === "sent").length;

  return (
    <>
      <div className="drawer lg:drawer-open">
        <input id="sidebar-drawer" type="checkbox" className="drawer-toggle" />
        <div className="drawer-content flex flex-col min-h-screen bg-base-200">
          <div className="navbar bg-base-100 shadow-sm px-4 lg:px-6">
            <div className="flex-none lg:hidden">
              <label htmlFor="sidebar-drawer" className="btn btn-square btn-ghost"><Menu size={20} /></label>
            </div>
            <div className="flex-1">
              <h1 className="text-lg font-semibold">{currentPage ? t(currentPage.labelKey) : t("app.name")}</h1>
            </div>
            <div className="flex-none flex items-center gap-1">
              <div className="tooltip tooltip-bottom" data-tip={streaming ? t("stream.on") : t("stream.off")}>
                <button className={`btn btn-ghost btn-circle ${streaming ? "text-success" : "text-base-content/30"}`} onClick={() => setStreaming(!streaming)}>
                  {streaming ? <Wifi size={18} /> : <WifiOff size={18} />}
                </button>
              </div>
              <button className="btn btn-ghost btn-circle" onClick={() => { setShowTracker(true); markAllRead(); }}>
                <div className="indicator">
                  <Bell size={18} />
                  {(unreadCount > 0 || pendingCommands > 0) && (
                    <span className="indicator-item badge badge-xs badge-error">
                      {pendingCommands > 0 ? pendingCommands : unreadCount > 99 ? "99+" : unreadCount}
                    </span>
                  )}
                </div>
              </button>
              <div className="dropdown dropdown-end">
                <div tabIndex={0} role="button" className="btn btn-ghost btn-circle"><Globe size={18} /></div>
                <ul tabIndex={0} className="dropdown-content menu bg-base-100 rounded-box z-10 w-40 p-2 shadow-lg border border-base-300">
                  {languages.map((lang) => (
                    <li key={lang.code}><button onClick={() => switchLang(lang.code)} className={i18n.language === lang.code ? "active" : ""}>{lang.label}</button></li>
                  ))}
                </ul>
              </div>
              <button className="btn btn-ghost btn-circle" onClick={toggleTheme}>
                {theme === "light" ? <Moon size={18} /> : <Sun size={18} />}
              </button>
              <div className="dropdown dropdown-end">
                <div tabIndex={0} role="button" className="btn btn-ghost gap-2">
                  <div className="avatar placeholder">
                    <div className="bg-primary text-primary-content w-8 rounded-full">
                      <span className="text-sm">{(user?.display_name || user?.username || "U")[0].toUpperCase()}</span>
                    </div>
                  </div>
                  <div className="hidden sm:block text-left">
                    <div className="text-sm font-medium">{user?.display_name || user?.username}</div>
                    <div className="text-xs opacity-60">{user?.system_role === "sys_admin" ? "sys_admin" : user?.role}</div>
                  </div>
                </div>
                <ul tabIndex={0} className="dropdown-content menu bg-base-100 rounded-box z-10 w-52 p-2 shadow-lg border border-base-300">
                  <li><button onClick={() => setShowChangePw(true)}><Lock size={16} />{t("changePassword.title")}</button></li>
                  <li><button onClick={logout} className="text-error"><LogOut size={16} />{t("common.logout")}</button></li>
                </ul>
              </div>
            </div>
          </div>
          <main className="flex-1 p-4 lg:p-6 overflow-auto"><Outlet /></main>
        </div>
        <div className="drawer-side z-40">
          <label htmlFor="sidebar-drawer" aria-label="close sidebar" className="drawer-overlay"></label>
          <aside className="bg-base-100 border-r border-base-300 w-64 min-h-screen flex flex-col">
            <div className="p-4 border-b border-base-300">
              <Link to="/dashboard" className="flex items-center gap-3">
                <div className="bg-primary text-primary-content w-10 h-10 rounded-lg flex items-center justify-center font-bold text-lg">M</div>
                <div>
                  <div className="font-bold text-lg">{t("app.name")}</div>
                  <div className="text-xs opacity-50">{t("app.tagline")}</div>
                </div>
              </Link>
            </div>
            <ul className="menu menu-md p-4 flex-1 gap-0.5" data-tour="nav-sidebar">
              {filteredGroups.map((group, gi) => (
                <li key={gi}>
                  {group.label && (
                    <div className="menu-title text-xs uppercase tracking-wider opacity-50 mt-3 mb-1 px-0">
                      {t(group.label)}
                    </div>
                  )}
                  <ul>
                    {group.items.map((item) => (
                      <li key={item.path}>
                        <Link to={item.path} className={location.pathname.startsWith(item.path) ? "active" : ""}>
                          {item.icon}{t(item.labelKey)}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
            <div className="p-4 border-t border-base-300 text-xs opacity-50 text-center">{t("app.version")}</div>
          </aside>
        </div>
      </div>
      <CommandTracker open={showTracker} onClose={() => setShowTracker(false)} />
      <ChangePassword open={showChangePw} onClose={() => setShowChangePw(false)} />
      <ViewerOnboarding />
    </>
  );
}
