import type { ReactNode } from "react";

import "./common.css";

interface ProgressRingProps {
  /** The fraction done, 0 to 1; undefined spins, for work of unknown length. */
  value?: number;
  /** The diameter in pixels. */
  size?: number;
  /** The accessible name of the progress bar. */
  label: string;
  /** Text in the middle, such as the percent or the bytes found. */
  children?: ReactNode;
}

const STROKE = 8;

/** ProgressRing is the overlays' ring: a filling arc when the fraction is known, the 维护 page's spinning arc when it is not. */
export function ProgressRing({ value, size = 96, label, children }: ProgressRingProps) {
  const r = 40;
  const circumference = 2 * Math.PI * r;
  const known = value !== undefined && Number.isFinite(value);
  const fraction = known ? Math.min(1, Math.max(0, value)) : 0;
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={known ? 0 : undefined}
      aria-valuemax={known ? 100 : undefined}
      aria-valuenow={known ? Math.round(fraction * 100) : undefined}
      className="progress-ring"
      style={{ width: size, height: size }}
    >
      <svg
        width={size}
        height={size}
        viewBox="0 0 96 96"
        aria-hidden="true"
        className={known ? undefined : "run-spinner"}
      >
        <circle className="run-spinner-track" cx="48" cy="48" r={r} fill="none" strokeWidth={STROKE} />
        {known ? (
          <circle
            className="progress-ring-fill"
            cx="48"
            cy="48"
            r={r}
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={circumference}
            strokeDashoffset={circumference * (1 - fraction)}
            transform="rotate(-90 48 48)"
          />
        ) : (
          <path
            className="run-spinner-arc"
            d="M48 8 A40 40 0 0 1 88 48"
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
          />
        )}
      </svg>
      {children !== undefined && <span className="progress-ring-center">{children}</span>}
    </div>
  );
}
