import type { SpaceEntry } from "../../mygo";
import {
  fileManagerName,
  formatBytes,
  formatRelativeTime,
  parentPath,
  trashName,
  truncateMiddle,
} from "../common/format";
import { Glyph } from "../common/Glyph";
import { entryLabel, fileCount, formatShare, isGroup, shareOf } from "./labels";

/** What a row does; the page decides how (opening a folder, confirming a move). */
export interface EntryActions {
  onOpen: (entry: SpaceEntry) => void;
  onReveal: (entry: SpaceEntry) => void;
  onTrash: (entry: SpaceEntry) => void;
}

interface EntryListProps extends EntryActions {
  /** The list's accessible name. */
  label: string;
  entries: readonly SpaceEntry[];
  /** share: each entry's share of total, with a bar (a folder's children); location: the folder holding it and its age (large files). */
  detail: "share" | "location";
  /** The folder's size, for the shares. */
  total?: number;
}

/** EntryList lists entries largest first, each with what it holds and its actions: 打开 for folders, 在访达中显示 (资源管理器) and 移到废纸篓 (回收站). An entry the page may not move keeps its button, which then says why. */
export function EntryList({ label, entries, detail, total = 0, ...actions }: EntryListProps) {
  return (
    <ul aria-label={label} className="space-list">
      {entries.map((e) => (
        <EntryRow
          key={isGroup(e) ? `group-${e.kind}` : e.path}
          entry={e}
          detail={detail}
          total={total}
          {...actions}
        />
      ))}
    </ul>
  );
}

const FOLDER = <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />;
const FILE = (
  <>
    <path d="M6 3h8l4 4v14H6z" />
    <path d="M14 3v4h4" />
  </>
);
const GROUP = (
  <>
    <circle cx="6" cy="12" r="1.2" />
    <circle cx="12" cy="12" r="1.2" />
    <circle cx="18" cy="12" r="1.2" />
  </>
);

interface EntryRowProps extends EntryActions {
  entry: SpaceEntry;
  detail: "share" | "location";
  total: number;
}

function EntryRow({ entry, detail, total, onOpen, onReveal, onTrash }: EntryRowProps) {
  const name = entryLabel(entry);
  const group = isGroup(entry);
  const share = shareOf(entry.size, total);
  const glyph = entry.kind === "dir" ? FOLDER : entry.kind === "file" ? FILE : GROUP;
  let sub: string;
  if (detail === "location") {
    sub = [truncateMiddle(parentPath(entry.path), 64), formatRelativeTime(entry.modTime ?? "")]
      .filter(Boolean)
      .join(" · ");
  } else if (entry.kind === "dir") {
    sub = entry.denied ? "无权读取" : `${formatShare(share)} · ${fileCount(entry.files)}`;
  } else {
    sub = formatShare(share);
  }
  return (
    <li className="space-row" title={entry.path || undefined}>
      <span aria-hidden="true" className={`space-row-icon space-row-icon--${entry.kind}`}>
        <Glyph size={16} color="currentColor">
          {glyph}
        </Glyph>
      </span>
      <div className="space-row-text">
        {entry.kind === "dir" ? (
          <button type="button" className="space-row-name space-row-open" onClick={() => onOpen(entry)}>
            {name}
          </button>
        ) : (
          <span className="space-row-name">{name}</span>
        )}
        <span className="space-row-sub">{sub}</span>
        {detail === "share" && (
          <span aria-hidden="true" className="space-share">
            <span className="space-share-fill" style={{ width: `${share * 100}%` }} />
          </span>
        )}
      </div>
      <span className="space-row-size">{formatBytes(entry.size)}</span>
      {group ? (
        <span className="space-row-actions" />
      ) : (
        <span className="space-row-actions">
          <button
            type="button"
            className="icon-button"
            aria-label={`在${fileManagerName()}中显示 ${name}`}
            title={`在${fileManagerName()}中显示`}
            onClick={() => onReveal(entry)}
          >
            <Glyph size={16} color="currentColor">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20l-4-4" />
            </Glyph>
          </button>
          <button
            type="button"
            className="icon-button space-trash-button"
            aria-label={`将 ${name} 移到${trashName()}`}
            aria-disabled={entry.locked ? true : undefined}
            title={entry.locked ?? `移到${trashName()}`}
            onClick={() => onTrash(entry)}
          >
            <Glyph size={16} color="currentColor">
              <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
            </Glyph>
          </button>
        </span>
      )}
    </li>
  );
}
