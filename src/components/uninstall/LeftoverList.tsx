import type { UninstallLeftover } from "../../mygo";
import { Checkbox } from "../common/Checkbox";
import { baseName, fileManagerName, formatBytes, parentPath, truncateMiddle } from "../common/format";
import { Glyph } from "../common/Glyph";
import { checkedTotals, KIND_LABELS } from "./model";

interface LeftoverListProps {
  leftovers: readonly UninstallLeftover[];
  checked: ReadonlySet<string>;
  onChange: (next: ReadonlySet<string>) => void;
  onReveal: (path: string) => void;
  disabled?: boolean;
}

/** LeftoverList lists what an app left behind, each with its checkbox, kind, place, size and a button that shows it in Finder or Explorer, under 全选 and 全不选. */
export function LeftoverList({
  leftovers,
  checked,
  onChange,
  onReveal,
  disabled = false,
}: LeftoverListProps) {
  const totals = checkedTotals(leftovers, checked);
  const toggle = (id: string, on: boolean) => {
    const next = new Set(checked);
    if (on) next.add(id);
    else next.delete(id);
    onChange(next);
  };
  return (
    <div className="uninstall-leftovers">
      <div className="category-toolbar">
        <span className="category-toolbar-text">{`已选 ${totals.count}/${leftovers.length} 项，共 ${formatBytes(totals.bytes)}`}</span>
        <button
          type="button"
          className="link-button"
          disabled={disabled}
          onClick={() => onChange(new Set(leftovers.map((l) => l.id)))}
        >
          全选
        </button>
        <button type="button" className="link-button" disabled={disabled} onClick={() => onChange(new Set())}>
          全不选
        </button>
      </div>
      <ul aria-label="相关文件" className="item-list uninstall-leftover-list">
        {leftovers.map((l) => {
          const name = baseName(l.path);
          const label = KIND_LABELS[l.kind];
          return (
            <li key={l.id} className="item-row uninstall-leftover-row" title={l.path}>
              <Checkbox
                checked={checked.has(l.id)}
                disabled={disabled}
                onChange={(on) => toggle(l.id, on)}
                aria-label={`${label}：${name}`}
              />
              <div className="item-text">
                <span className="item-name">
                  <span className="uninstall-kind">{label}</span>
                  {name}
                </span>
                <span className="item-path">
                  {[truncateMiddle(parentPath(l.path), 60), l.note].filter(Boolean).join(" · ")}
                </span>
              </div>
              <span className="item-size">{formatBytes(l.size)}</span>
              <button
                type="button"
                className="icon-button"
                aria-label={`在${fileManagerName()}中显示 ${name}`}
                title={`在${fileManagerName()}中显示`}
                onClick={() => onReveal(l.path)}
              >
                <Glyph size={16} color="currentColor">
                  <circle cx="11" cy="11" r="7" />
                  <path d="M20 20l-4-4" />
                </Glyph>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
