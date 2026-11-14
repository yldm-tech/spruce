import { useId, type ReactNode } from "react";

import { IconTile, type Tone } from "../common/Glyph";

interface VitalCardProps {
  title: string;
  tone: Tone;
  /** The glyph's paths on the 24-unit grid. */
  icon: ReactNode;
  /** The headline figure, e.g. "23%"; null while there is no sample yet, false when the metric could not be read. */
  value: ReactNode | null | false;
  /** A short line after the headline, e.g. "已用 24 GB，共 36 GB". */
  caption?: ReactNode;
  children?: ReactNode;
}

/** VitalCard is one overview card of the 健康监测 page: a gradient tile and title, the headline figure, and details below. It says 正在读取… before the first sample and 无法读取 when its metric failed. */
export function VitalCard({ title, tone, icon, value, caption, children }: VitalCardProps) {
  const titleId = useId();
  const unreadable = value === false;
  return (
    <section aria-labelledby={titleId} className="vital-card">
      <div className="vital-card-head">
        <IconTile tone={tone} size={36} glyphSize={18} className="vital-card-icon">
          {icon}
        </IconTile>
        <h2 id={titleId} className="vital-card-title">
          {title}
        </h2>
      </div>
      {value === null ? (
        <span className="vital-card-value vital-card-value--pending">正在读取…</span>
      ) : unreadable ? (
        <span className="vital-card-value vital-card-value--pending">无法读取</span>
      ) : (
        <>
          <span className="vital-card-value">{value}</span>
          {caption && <span className="vital-card-caption">{caption}</span>}
          {children}
        </>
      )}
    </section>
  );
}

/** DetailList shows label and value pairs under a card's headline. */
export function DetailList({ rows }: { rows: readonly [label: string, value: ReactNode][] }) {
  return (
    <dl className="vital-details">
      {rows.map(([label, value]) => (
        <div key={label} className="vital-detail">
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

interface MeterProps {
  /** 0 to 100. */
  value: number;
  label: string;
  /** What a screen reader says for the value, e.g. "已用 62%". */
  valueText: string;
  /** Turns the fill to the warning or danger colour. */
  level?: "normal" | "warning" | "critical";
}

/** Meter is a thin horizontal bar for a share of a whole: disk used, memory used, battery charge. */
export function Meter({ value, label, valueText, level = "normal" }: MeterProps) {
  const v = Math.min(100, Math.max(0, value));
  return (
    <div
      role="meter"
      aria-label={label}
      aria-valuenow={Math.round(v)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuetext={valueText}
      className={`meter meter--${level}`}
    >
      <span className="meter-fill" style={{ width: `${v}%` }} />
    </div>
  );
}
