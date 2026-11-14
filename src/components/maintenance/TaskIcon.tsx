import type { ReactNode } from "react";

import type { Icon } from "../../mygo";

interface Glyph {
  /** A --gradient-* token of tokens.css. */
  gradient: string;
  /** stroke-linejoin round, as the design draws the arrow glyph. */
  roundJoin?: boolean;
  paths: ReactNode;
}

// The first six glyphs are the design's macOS cards; the others follow the same 24-unit grid and 2px stroke for the Windows catalog.
const GLYPHS: Record<Icon, Glyph> = {
  network: {
    gradient: "--gradient-blue",
    paths: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" />
      </>
    ),
  },
  search: {
    gradient: "--gradient-violet",
    paths: (
      <>
        <circle cx="11" cy="11" r="7" />
        <path d="M20 20l-4-4" />
      </>
    ),
  },
  preview: {
    gradient: "--gradient-cyan",
    paths: (
      <>
        <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z" />
        <circle cx="12" cy="12" r="3" />
      </>
    ),
  },
  icons: {
    gradient: "--gradient-pink",
    paths: (
      <>
        <rect x="4" y="4" width="16" height="16" rx="4" />
        <path d="M9 9h6v6H9z" />
      </>
    ),
  },
  "open-with": {
    gradient: "--gradient-green",
    roundJoin: true,
    paths: <path d="M4 12h16M14 6l6 6-6 6" />,
  },
  database: {
    gradient: "--gradient-amber",
    paths: (
      <>
        <ellipse cx="12" cy="6" rx="7" ry="3" />
        <path d="M5 6v12c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12c0 1.7 3.1 3 7 3s7-1.3 7-3" />
      </>
    ),
  },
  cleanup: {
    gradient: "--gradient-green",
    roundJoin: true,
    paths: (
      <>
        <path d="M14 3l-3 8" />
        <path d="M5 12h12l1.5 8h-15z" />
      </>
    ),
  },
  repair: {
    gradient: "--gradient-orange",
    roundJoin: true,
    paths: <path d="M15 4a5 5 0 0 0-5.6 6.6L4 16v4h4l5.4-5.4A5 5 0 0 0 20 9l-3 3-3-1-1-3z" />,
  },
  disk: {
    gradient: "--gradient-cyan",
    roundJoin: true,
    paths: (
      <>
        <path d="M5 13l2.5-8h9L19 13" />
        <rect x="3" y="13" width="18" height="7" rx="2" />
        <path d="M7 16.5h.01M11 16.5h6" />
      </>
    ),
  },
  update: {
    gradient: "--gradient-blue",
    roundJoin: true,
    paths: (
      <>
        <path d="M19.5 9A8 8 0 0 0 5 7.5L4 9" />
        <path d="M4 4v5h5" />
        <path d="M4.5 15A8 8 0 0 0 19 16.5l1-1.5" />
        <path d="M20 20v-5h-5" />
      </>
    ),
  },
  font: {
    gradient: "--gradient-violet",
    roundJoin: true,
    paths: <path d="M5 20l7-16 7 16M8 14h8" />,
  },
  store: {
    gradient: "--gradient-pink",
    roundJoin: true,
    paths: (
      <>
        <path d="M5 8h14l-1 12H6z" />
        <path d="M9 8V6a3 3 0 0 1 6 0v2" />
      </>
    ),
  },
  "network-fix": {
    gradient: "--gradient-red",
    roundJoin: true,
    paths: (
      <>
        <path d="M9 3v5M15 3v5" />
        <path d="M6 8h12v3a6 6 0 0 1-12 0z" />
        <path d="M12 17v4" />
      </>
    ),
  },
};

/** TaskIcon is the 42px gradient tile at the top left of a maintenance card. */
export function TaskIcon({ icon }: { icon: Icon }) {
  const glyph = GLYPHS[icon];
  return (
    <span aria-hidden="true" className="task-icon" style={{ background: `var(${glyph.gradient})` }}>
      <svg
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="#FFFFFF"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin={glyph.roundJoin ? "round" : undefined}
      >
        {glyph.paths}
      </svg>
    </span>
  );
}
