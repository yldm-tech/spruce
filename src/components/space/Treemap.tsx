import { useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from "react";

import type { SpaceEntry } from "../../mygo";
import { formatBytes } from "../common/format";
import { entryLabel, formatShare, isGroup, shareOf } from "./labels";
import { cellShowsLabel, layoutTreemap, type TreemapCell } from "./treemapLayout";

/** How many cell colours tokens.css defines (--space-cell-1 …). */
const TONES = 6;

interface TreemapProps {
  /** The children of the folder shown, largest first. */
  entries: readonly SpaceEntry[];
  /** The folder's size, for the share in each tooltip. */
  total: number;
  /** The folder's name, for the map's accessible name. */
  label: string;
  /** Opens a folder; cells of folders are buttons that call it. */
  onOpen: (entry: SpaceEntry) => void;
}

/** Treemap draws the children of a folder as a squarified treemap (layoutTreemap) of absolutely positioned cells: a cell's area is its share of the folder. Folders are buttons that open them; large cells show the name and size, the others show them as a tooltip. The list beside the map holds the same entries for the keyboard and screen readers, so the map's cells are left out of the tab order. */
export function Treemap({ entries, total, label, onOpen }: TreemapProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setSize({ width: el.clientWidth, height: el.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const cells = useMemo(
    () => layoutTreemap(entries, (e) => e.size, size.width, size.height),
    [entries, size.width, size.height],
  );
  return (
    <div ref={ref} className="space-treemap" role="group" aria-label={`${label} 的空间分布图`}>
      {cells.map((cell, i) => (
        <Cell key={cellKey(cell.item)} cell={cell} tone={(i % TONES) + 1} total={total} onOpen={onOpen} />
      ))}
    </div>
  );
}

function cellKey(entry: SpaceEntry): string {
  return isGroup(entry) ? `group-${entry.kind}` : entry.path;
}

function Cell({
  cell,
  tone,
  total,
  onOpen,
}: {
  cell: TreemapCell<SpaceEntry>;
  tone: number;
  total: number;
  onOpen: (entry: SpaceEntry) => void;
}) {
  const entry = cell.item;
  const name = entryLabel(entry);
  const size = formatBytes(entry.size);
  const tip = `${name} · ${size} · ${formatShare(shareOf(entry.size, total))}`;
  const style: CSSProperties = {
    left: cell.x,
    top: cell.y,
    width: cell.width,
    height: cell.height,
    background: isGroup(entry) ? undefined : `var(--space-cell-${tone})`,
  };
  const body = cellShowsLabel(cell) && (
    <>
      <span className="space-cell-name">{name}</span>
      <span className="space-cell-size">{size}</span>
    </>
  );
  const className = isGroup(entry) ? "space-cell space-cell--group" : "space-cell";
  if (entry.kind === "dir") {
    return (
      <button
        type="button"
        tabIndex={-1}
        className={`${className} space-cell--open`}
        style={style}
        title={`${tip}\n点按打开`}
        aria-label={`打开 ${name}，${size}`}
        onClick={() => onOpen(entry)}
      >
        {body}
      </button>
    );
  }
  return (
    <div className={className} style={style} title={tip} aria-hidden="true">
      {body}
    </div>
  );
}
