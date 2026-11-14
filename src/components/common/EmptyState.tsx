import type { ReactNode } from "react";

import { IconTile, type Tone } from "./Glyph";

import "./common.css";

interface EmptyStateProps {
  title: string;
  description?: string;
  /** A tile above the title: its tone and glyph paths. */
  tone?: Tone;
  icon?: ReactNode;
  /** A button or link below the text. */
  action?: ReactNode;
}

/** EmptyState fills a page or a list that has nothing to show, e.g. "没有找到可以清理的内容". */
export function EmptyState({ title, description, tone = "green", icon, action }: EmptyStateProps) {
  return (
    <div className="empty-state">
      {icon && (
        <IconTile tone={tone} size={56} glyphSize={26} className="empty-state-tile">
          {icon}
        </IconTile>
      )}
      <span className="empty-state-title">{title}</span>
      {description && <span className="empty-state-description">{description}</span>}
      {action}
    </div>
  );
}
