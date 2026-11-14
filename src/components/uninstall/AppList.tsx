import { useVirtualizer } from "@tanstack/react-virtual";
import { useId, useRef, type CSSProperties, type KeyboardEvent } from "react";

import type { UninstallApp } from "../../mygo";
import { AppIcon } from "./AppIcon";
import { sizeText, usageText, type AppDetails, type SortKey } from "./model";

/** Lists longer than this are virtualized. */
const VIRTUALIZE_AFTER = 60;
/** The height of one row, which the virtualized list relies on. */
const ROW_HEIGHT = 60;

interface AppListProps {
  /** The apps to show, already filtered and sorted. */
  apps: readonly UninstallApp[];
  /** How many apps there are before the search. */
  total: number;
  details: ReadonlyMap<string, AppDetails>;
  measuring: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
  query: string;
  onQueryChange: (query: string) => void;
  sort: SortKey;
  onSortChange: (sort: SortKey) => void;
  windows: boolean;
}

/** AppList is the left column of the page: the search box, the order and the apps as a single-selection list box (arrow keys, Home and End move the selection). Long lists are virtualized. */
export function AppList(props: AppListProps) {
  const { apps, total, query, onQueryChange, sort, onSortChange, windows } = props;
  const sorts: { key: SortKey; label: string }[] = [
    { key: "name", label: "名称" },
    { key: "size", label: "大小" },
    { key: "recent", label: windows ? "安装时间" : "最近使用" },
  ];
  return (
    <section aria-label="已安装的应用" className="uninstall-list-card">
      <div className="uninstall-list-tools">
        <input
          type="search"
          className="uninstall-search"
          placeholder="搜索应用"
          aria-label="搜索应用"
          value={query}
          onChange={(e) => onQueryChange(e.currentTarget.value)}
        />
        <div role="group" aria-label="排序方式" className="uninstall-sort">
          {sorts.map((s) => (
            <button
              key={s.key}
              type="button"
              aria-pressed={sort === s.key}
              className="uninstall-sort-button"
              onClick={() => onSortChange(s.key)}
            >
              {s.label}
            </button>
          ))}
        </div>
      </div>
      <span className="uninstall-list-count" aria-live="polite">
        {query.trim() ? `找到 ${apps.length} 个，共 ${total} 个应用` : `共 ${total} 个应用`}
      </span>
      {apps.length === 0 ? (
        <span className="uninstall-list-empty">
          {query.trim() ? "没有符合搜索的应用" : "没有可以卸载的应用"}
        </span>
      ) : (
        <Rows {...props} />
      )}
    </section>
  );
}

function Rows({ apps, details, measuring, selectedId, onSelect, windows }: AppListProps) {
  const listId = useId();
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtual = apps.length > VIRTUALIZE_AFTER;
  // The app does not use the React Compiler, which is what this rule protects: it would skip memoizing this component, which nothing here relies on.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: virtual ? apps.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
  });
  const optionId = (id: string) => `${listId}-${id}`;
  const index = apps.findIndex((a) => a.id === selectedId);

  const move = (to: number) => {
    const next = apps[Math.max(0, Math.min(apps.length - 1, to))];
    if (!next) return;
    onSelect(next.id);
    const i = apps.indexOf(next);
    if (virtual) virtualizer.scrollToIndex(i, { align: "auto" });
    else document.getElementById(optionId(next.id))?.scrollIntoView?.({ block: "nearest" });
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const keys: Record<string, number> = {
      ArrowDown: index < 0 ? 0 : index + 1,
      ArrowUp: index < 0 ? 0 : index - 1,
      Home: 0,
      End: apps.length - 1,
      PageDown: index + 8,
      PageUp: index - 8,
    };
    const to = keys[e.key];
    if (to === undefined) return;
    e.preventDefault();
    move(to);
  };

  const row = (app: UninstallApp, style?: CSSProperties, position?: number) => (
    <AppRow
      key={app.id}
      id={optionId(app.id)}
      app={app}
      details={details.get(app.id)}
      measuring={measuring}
      selected={app.id === selectedId}
      onSelect={onSelect}
      windows={windows}
      style={style}
      setSize={virtual ? apps.length : undefined}
      position={position}
    />
  );

  return (
    <div ref={scrollRef} className="uninstall-list-scroll">
      <div
        role="listbox"
        aria-label="应用"
        tabIndex={0}
        aria-activedescendant={selectedId && index >= 0 ? optionId(selectedId) : undefined}
        onKeyDown={onKeyDown}
        className="uninstall-listbox"
        style={virtual ? { height: virtualizer.getTotalSize(), position: "relative" } : undefined}
      >
        {virtual
          ? virtualizer.getVirtualItems().map((v) => {
              const app = apps[v.index];
              if (!app) return null;
              return row(
                app,
                { position: "absolute", top: 0, left: 0, right: 0, transform: `translateY(${v.start}px)` },
                v.index + 1,
              );
            })
          : apps.map((a) => row(a))}
      </div>
    </div>
  );
}

interface AppRowProps {
  id: string;
  app: UninstallApp;
  details: AppDetails | undefined;
  measuring: boolean;
  selected: boolean;
  onSelect: (id: string) => void;
  windows: boolean;
  style?: CSSProperties;
  setSize?: number;
  position?: number;
}

function AppRow({
  id,
  app,
  details,
  measuring,
  selected,
  onSelect,
  windows,
  style,
  setSize,
  position,
}: AppRowProps) {
  const sub = [app.version, usageText(app, details, windows)].filter(Boolean).join(" · ");
  return (
    // The list box takes the keys (arrow keys move the selection); a row is picked with the pointer here.
    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events
    <div
      id={id}
      role="option"
      tabIndex={-1}
      aria-selected={selected}
      aria-setsize={setSize}
      aria-posinset={position}
      className={selected ? "uninstall-row uninstall-row--selected" : "uninstall-row"}
      style={style}
      onClick={() => onSelect(app.id)}
    >
      <AppIcon icon={details?.icon} size={36} />
      <span className="uninstall-row-text">
        <span className="uninstall-row-name">{app.name}</span>
        {sub && <span className="uninstall-row-sub">{sub}</span>}
      </span>
      {app.appStore && <span className="uninstall-badge">App Store</span>}
      {app.running && <span className="uninstall-badge uninstall-badge--running">运行中</span>}
      <span className="uninstall-row-size">{sizeText(details, measuring)}</span>
    </div>
  );
}
