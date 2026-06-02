import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AgGridReact } from "ag-grid-react";
import type { AgGridReactProps } from "ag-grid-react";
import { themeQuartz, colorSchemeDark, colorSchemeLight, type ColDef } from "ag-grid-enterprise";
// Official AG Grid Traditional Chinese pack — replaces the hand-rolled
// dictionary we maintained before. Covers ~all UI strings including the
// ones we kept forgetting (column menu, side bar, filter operators, etc.).
import { AG_GRID_LOCALE_TW } from "@ag-grid-community/locale";

const LOCALE_ZH_TW = AG_GRID_LOCALE_TW;
const LOCALE_EN: Record<string, string> = {};

interface DataGridProps<T> extends Omit<AgGridReactProps<T>, "theme"> {
  /** CSS height — defaults to a comfortable full-card height. */
  height?: string | number;
  /** Disable the sidebar tool panels (columns/filters). */
  hideSidebar?: boolean;
}

export function DataGrid<T>({
  height = "calc(100vh - 16rem)",
  hideSidebar,
  defaultColDef,
  rowSelection,
  pagination = true,
  paginationPageSize = 50,
  paginationPageSizeSelector = [20, 50, 100, 200],
  sideBar,
  detailCellRendererParams,
  ...rest
}: DataGridProps<T>) {
  const { i18n } = useTranslation();
  const [isDark, setIsDark] = useState(
    () => document.documentElement.getAttribute("data-theme") === "dark",
  );
  const observerRef = useRef<MutationObserver | null>(null);

  // Track <html data-theme> changes so the grid re-themes live with the app.
  useEffect(() => {
    const el = document.documentElement;
    const check = () => setIsDark(el.getAttribute("data-theme") === "dark");
    check();
    observerRef.current = new MutationObserver(check);
    observerRef.current.observe(el, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observerRef.current?.disconnect();
  }, []);

  const theme = useMemo(
    () =>
      themeQuartz.withPart(isDark ? colorSchemeDark : colorSchemeLight).withParams({
        fontFamily: "inherit",
        borderRadius: 6,
        headerHeight: 38,
        rowHeight: 36,
      }),
    [isDark],
  );

  const mergedDefaultColDef: ColDef = useMemo(
    () => ({
      sortable: true,
      filter: true,
      resizable: true,
      floatingFilter: false,
      minWidth: 80,
      flex: 1,
      ...defaultColDef,
    }),
    [defaultColDef],
  );

  const resolvedSidebar =
    sideBar !== undefined
      ? sideBar
      : hideSidebar
        ? false
        : {
            toolPanels: [
              {
                id: "columns",
                labelDefault: "Columns",
                labelKey: "columns",
                iconKey: "columns",
                toolPanel: "agColumnsToolPanel",
                toolPanelParams: { suppressRowGroups: true, suppressValues: true, suppressPivots: true },
              },
              {
                id: "filters",
                labelDefault: "Filters",
                labelKey: "filters",
                iconKey: "filter",
                toolPanel: "agFiltersToolPanel",
              },
            ],
          };

  const localeText = i18n.language === "en" ? LOCALE_EN : LOCALE_ZH_TW;

  // Master/detail uses a SEPARATE inner AG Grid instance. It does NOT inherit
  // localeText from the parent, so we have to inject it into the caller's
  // detailGridOptions ourselves. Otherwise the inner grid falls back to
  // English (e.g. "No Rows To Show", column menu strings).
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const mergedDetailParams = useMemo<any>(() => {
    if (!detailCellRendererParams) return undefined;
    const params = detailCellRendererParams as { detailGridOptions?: Record<string, unknown> } & Record<string, unknown>;
    return {
      ...params,
      detailGridOptions: {
        ...(params.detailGridOptions || {}),
        localeText: { ...localeText, ...((params.detailGridOptions?.localeText as Record<string, string>) || {}) },
      },
    };
  }, [detailCellRendererParams, localeText]);

  return (
    <div style={{ width: "100%", height }}>
      <AgGridReact<T>
        theme={theme}
        defaultColDef={mergedDefaultColDef}
        rowSelection={rowSelection}
        pagination={pagination}
        paginationPageSize={paginationPageSize}
        paginationPageSizeSelector={paginationPageSizeSelector}
        sideBar={resolvedSidebar}
        localeText={localeText}
        animateRows
        suppressCellFocus
        detailCellRendererParams={mergedDetailParams}
        {...rest}
      />
    </div>
  );
}
