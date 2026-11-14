import { useVirtualizer } from "@tanstack/react-virtual";
import { useId, useRef, useState, type CSSProperties } from "react";

import type { Category, Item } from "../../mygo";
import { CategoryIcon } from "./CategoryIcon";
import { Checkbox } from "./Checkbox";
import { useReveal } from "./files";
import {
  baseName,
  fileManagerName,
  formatBytes,
  formatRelativeTime,
  parentPath,
  truncateMiddle,
} from "./format";
import { Glyph } from "./Glyph";
import {
  categoryState,
  NOTHING_SELECTED,
  selectAll,
  selectedTotals,
  setCategory,
  setItem,
  type Selection,
} from "./selection";

import "./common.css";

interface CategoryListProps {
  categories: readonly Category[];
  selection: Selection;
  onSelectionChange: (next: Selection) => void;
  /** The list's accessible name, e.g. "可以清理的项目". */
  label: string;
  /** Shows path in the file manager; by default Files.reveal. */
  onReveal?: (path: string) => void;
  /** Disables every checkbox, e.g. while a clean runs. */
  disabled?: boolean;
}

/** Lists longer than this are virtualized. */
const VIRTUALIZE_AFTER = 60;
/** The height of one item row, which the virtualized list relies on. */
const ROW_HEIGHT = 48;

/** CategoryList is the result list of the cleaner pages: one glass card per category with its tri-state checkbox, icon, size and warning, expanding to its items, each with a checkbox, size, age and a button that shows it in Finder or Explorer. 全选 and 全不选 act on every category. Long item lists are virtualized. Selection is controlled; see selection.ts for the helpers. */
export function CategoryList({
  categories,
  selection,
  onSelectionChange,
  label,
  onReveal,
  disabled = false,
}: CategoryListProps) {
  const reveal = useReveal();
  const doReveal = onReveal ?? ((path: string) => reveal.mutate(path));
  const totals = selectedTotals(categories, selection);
  const total = categories.reduce((sum, c) => sum + c.size, 0);
  return (
    <section aria-label={label} className="category-list">
      <div className="category-toolbar">
        <span className="category-toolbar-text">{`${categories.length} 类，共 ${formatBytes(total)}，已选 ${totals.count} 项`}</span>
        <button
          type="button"
          className="link-button"
          disabled={disabled}
          onClick={() => onSelectionChange(selectAll(categories))}
        >
          全选
        </button>
        <button
          type="button"
          className="link-button"
          disabled={disabled}
          onClick={() => onSelectionChange(NOTHING_SELECTED)}
        >
          全不选
        </button>
        {reveal.isError && (
          <span role="alert" className="category-toolbar-error">
            {reveal.error instanceof Error ? reveal.error.message : `无法在${fileManagerName()}中显示`}
          </span>
        )}
      </div>
      {categories.map((c) => (
        <CategoryCard
          key={c.id}
          category={c}
          selection={selection}
          onSelectionChange={onSelectionChange}
          onReveal={doReveal}
          disabled={disabled}
        />
      ))}
    </section>
  );
}

interface CardProps {
  category: Category;
  selection: Selection;
  onSelectionChange: (next: Selection) => void;
  onReveal: (path: string) => void;
  disabled: boolean;
}

function CategoryCard({ category, selection, onSelectionChange, onReveal, disabled }: CardProps) {
  const [open, setOpen] = useState(false);
  const titleId = useId();
  const textId = useId();
  const listId = useId();
  const state = categoryState(category, selection);
  const checked = selection.get(category.id);
  const empty = category.items.length === 0;
  return (
    <div className="category-card" data-category-id={category.id}>
      <div className="category-head">
        <Checkbox
          checked={state === "all"}
          indeterminate={state === "some"}
          disabled={disabled || empty}
          onChange={(on) => onSelectionChange(setCategory(selection, category, on))}
          aria-labelledby={titleId}
          aria-describedby={textId}
        />
        <CategoryIcon icon={category.icon} />
        <div className="category-text">
          <span id={titleId} className="category-title">
            {category.title}
          </span>
          <span id={textId} className={category.warning ? "category-warning" : "category-description"}>
            {category.warning ?? category.description}
          </span>
        </div>
        <div className="category-figures">
          <span className="category-size">{formatBytes(category.size)}</span>
          <span className="category-count">
            {state === "some"
              ? `已选 ${checked?.size ?? 0}/${category.items.length} 项`
              : `${category.items.length} 项`}
          </span>
        </div>
        <button
          type="button"
          className="icon-button category-expand"
          aria-expanded={open}
          aria-controls={listId}
          aria-label={`${open ? "收起" : "展开"}${category.title}`}
          disabled={empty}
          onClick={() => setOpen((v) => !v)}
        >
          <Glyph size={16} color="currentColor">
            <path d={open ? "M6 15l6-6 6 6" : "M6 9l6 6 6-6"} />
          </Glyph>
        </button>
      </div>
      {open && (
        <ItemList
          id={listId}
          label={category.title}
          items={category.items}
          isChecked={(id) => checked?.has(id) ?? false}
          onToggle={(id, on) => onSelectionChange(setItem(selection, category, id, on))}
          onReveal={onReveal}
          disabled={disabled}
        />
      )}
    </div>
  );
}

interface ItemListProps {
  id: string;
  label: string;
  items: readonly Item[];
  isChecked: (id: string) => boolean;
  onToggle: (id: string, on: boolean) => void;
  onReveal: (path: string) => void;
  disabled: boolean;
}

function ItemList(props: ItemListProps) {
  return props.items.length > VIRTUALIZE_AFTER ? (
    <VirtualItemList {...props} />
  ) : (
    <PlainItemList {...props} />
  );
}

function PlainItemList({ id, label, items, ...row }: ItemListProps) {
  return (
    <ul id={id} aria-label={label} className="item-list">
      {items.map((item) => (
        <ItemRow key={item.id} item={item} {...row} />
      ))}
    </ul>
  );
}

function VirtualItemList({ id, label, items, ...row }: ItemListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  // The app does not use the React Compiler, which is what this rule protects: it would skip memoizing this component, which nothing here relies on.
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
  });
  return (
    <div ref={scrollRef} className="item-list-scroll">
      <ul
        id={id}
        aria-label={label}
        className="item-list"
        style={{ height: virtualizer.getTotalSize(), position: "relative" }}
      >
        {virtualizer.getVirtualItems().map((v) => {
          const item = items[v.index];
          if (!item) return null;
          return (
            <ItemRow
              key={item.id}
              item={item}
              {...row}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                transform: `translateY(${v.start}px)`,
              }}
              setSize={items.length}
              position={v.index + 1}
            />
          );
        })}
      </ul>
    </div>
  );
}

interface ItemRowProps {
  item: Item;
  isChecked: (id: string) => boolean;
  onToggle: (id: string, on: boolean) => void;
  onReveal: (path: string) => void;
  disabled: boolean;
  style?: CSSProperties;
  /** For virtualized rows, which are not all in the document: the list's length and this row's place. */
  setSize?: number;
  position?: number;
}

function ItemRow({ item, isChecked, onToggle, onReveal, disabled, style, setSize, position }: ItemRowProps) {
  const name = baseName(item.path);
  const where = parentPath(item.path);
  const age = formatRelativeTime(item.modTime);
  return (
    <li className="item-row" style={style} aria-setsize={setSize} aria-posinset={position} title={item.path}>
      <Checkbox
        checked={isChecked(item.id)}
        disabled={disabled}
        onChange={(on) => onToggle(item.id, on)}
        aria-label={name}
      />
      <div className="item-text">
        <span className="item-name">{name}</span>
        <span className="item-path">
          {[truncateMiddle(where, 72), item.note].filter(Boolean).join(" · ")}
        </span>
      </div>
      {age && <span className="item-age">{age}</span>}
      <span className="item-size">{formatBytes(item.size)}</span>
      <button
        type="button"
        className="icon-button"
        aria-label={`在${fileManagerName()}中显示 ${name}`}
        title={`在${fileManagerName()}中显示`}
        onClick={() => onReveal(item.path)}
      >
        <Glyph size={16} color="currentColor">
          <circle cx="11" cy="11" r="7" />
          <path d="M20 20l-4-4" />
        </Glyph>
      </button>
    </li>
  );
}
