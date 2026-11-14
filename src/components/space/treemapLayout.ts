import { hierarchy, treemap, treemapSquarify } from "d3-hierarchy";

/** A rectangle of the treemap, in pixels from the top left of the map. */
export interface TreemapCell<T> {
  item: T;
  x: number;
  y: number;
  width: number;
  height: number;
}

interface Datum<T> {
  item?: T;
  children?: Datum<T>[];
}

/** The gap between neighbouring cells, in pixels. */
export const TREEMAP_GAP = 3;

/** layoutTreemap lays items out as a squarified treemap (d3-hierarchy's treemapSquarify) filling width by height, one cell per item with a positive size, largest first, with TREEMAP_GAP between cells and edges rounded to whole pixels. Items of no size and an empty area give no cells. */
export function layoutTreemap<T>(
  items: readonly T[],
  sizeOf: (item: T) => number,
  width: number,
  height: number,
): TreemapCell<T>[] {
  const sized = items.filter((item) => sizeOf(item) > 0);
  if (sized.length === 0 || width <= 0 || height <= 0) return [];
  const root = hierarchy<Datum<T>>({ children: sized.map((item) => ({ item })) })
    .sum((d) => (d.item === undefined ? 0 : sizeOf(d.item)))
    .sort((a, b) => (b.value ?? 0) - (a.value ?? 0));
  const laid = treemap<Datum<T>>()
    .tile(treemapSquarify)
    .size([width, height])
    .paddingInner(TREEMAP_GAP)
    .round(true)(root);
  const cells: TreemapCell<T>[] = [];
  for (const leaf of laid.leaves()) {
    const item = leaf.data.item;
    if (item === undefined) continue;
    const cellWidth = leaf.x1 - leaf.x0;
    const cellHeight = leaf.y1 - leaf.y0;
    // A sliver that rounds away to nothing is not drawn; the list beside the map still shows it.
    if (cellWidth <= 0 || cellHeight <= 0) continue;
    cells.push({ item, x: leaf.x0, y: leaf.y0, width: cellWidth, height: cellHeight });
  }
  return cells;
}

/** Cells at least this large show their name and size; smaller ones show them as a tooltip. */
export const LABEL_MIN_WIDTH = 72;
export const LABEL_MIN_HEIGHT = 38;

/** cellShowsLabel reports whether a cell is large enough to be labelled. */
export function cellShowsLabel(cell: { width: number; height: number }): boolean {
  return cell.width >= LABEL_MIN_WIDTH && cell.height >= LABEL_MIN_HEIGHT;
}
