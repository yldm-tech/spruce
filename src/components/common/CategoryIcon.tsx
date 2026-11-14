import type { ReactNode } from "react";

import { IconTile, type Tone } from "./Glyph";

interface CategoryGlyph {
  tone: Tone;
  paths: ReactNode;
}

const folder = <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />;

/** The glyphs a Category.icon key may name, on the same 24-unit grid as the sidebar. A key not listed falls back to the folder. */
const GLYPHS: Record<string, CategoryGlyph> = {
  cache: {
    tone: "green",
    paths: (
      <>
        <ellipse cx="12" cy="6" rx="7" ry="3" />
        <path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6" />
      </>
    ),
  },
  log: {
    tone: "blue",
    paths: (
      <>
        <path d="M6 3h9l4 4v14H6z" />
        <path d="M9 11h7M9 15h7M9 7h3" />
      </>
    ),
  },
  crash: {
    tone: "red",
    paths: (
      <>
        <path d="M12 4l9 16H3z" />
        <path d="M12 10v4M12 17v.5" />
      </>
    ),
  },
  temp: {
    tone: "cyan",
    paths: (
      <>
        <circle cx="12" cy="13" r="7" />
        <path d="M12 9v4l2.5 2M10 3h4" />
      </>
    ),
  },
  trash: {
    tone: "slate",
    paths: (
      <>
        <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
      </>
    ),
  },
  browser: {
    tone: "blue",
    paths: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" />
      </>
    ),
  },
  mail: {
    tone: "cyan",
    paths: (
      <>
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <path d="M3 7l9 6 9-6" />
      </>
    ),
  },
  update: {
    tone: "violet",
    paths: (
      <>
        <path d="M19.5 9A8 8 0 0 0 5 7.5L4 9" />
        <path d="M4 4v5h5" />
        <path d="M4.5 15A8 8 0 0 0 19 16.5l1-1.5" />
        <path d="M20 20v-5h-5" />
      </>
    ),
  },
  xcode: {
    tone: "blue",
    paths: (
      <>
        <path d="M15 4a5 5 0 0 0-5.6 6.6L4 16v4h4l5.4-5.4A5 5 0 0 0 20 9l-3 3-3-1-1-3z" />
      </>
    ),
  },
  simulator: {
    tone: "slate",
    paths: (
      <>
        <rect x="7" y="2" width="10" height="20" rx="2.5" />
        <path d="M11 18h2" />
      </>
    ),
  },
  node: {
    tone: "green",
    paths: (
      <>
        <path d="M12 2l9 5v10l-9 5-9-5V7z" />
        <path d="M12 22V12M21 7l-9 5-9-5" />
      </>
    ),
  },
  package: {
    tone: "orange",
    paths: (
      <>
        <path d="M3 7l9-4 9 4v10l-9 4-9-4z" />
        <path d="M3 7l9 4 9-4M12 11v10" />
      </>
    ),
  },
  docker: {
    tone: "cyan",
    paths: (
      <>
        <rect x="3" y="10" width="18" height="8" rx="2" />
        <path d="M6 10V7h4v3M10 10V5h4v5M14 10V7h4v3" />
      </>
    ),
  },
  build: {
    tone: "orange",
    paths: (
      <>
        <path d="M8 7l-5 5 5 5M16 7l5 5-5 5" />
      </>
    ),
  },
  installer: {
    tone: "pink",
    paths: (
      <>
        <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />
      </>
    ),
  },
  disk: {
    tone: "cyan",
    paths: (
      <>
        <path d="M5 13l2.5-8h9L19 13" />
        <rect x="3" y="13" width="18" height="7" rx="2" />
        <path d="M7 16.5h.01M11 16.5h6" />
      </>
    ),
  },
  app: {
    tone: "blue",
    paths: (
      <>
        <rect x="4" y="4" width="16" height="16" rx="4" />
        <path d="M9 9h6v6H9z" />
      </>
    ),
  },
  leftover: {
    tone: "violet",
    paths: (
      <>
        <rect x="4" y="4" width="7" height="7" rx="2" />
        <rect x="13" y="4" width="7" height="7" rx="2" />
        <rect x="4" y="13" width="7" height="7" rx="2" />
        <path d="M14 16.5h5" />
      </>
    ),
  },
  file: {
    tone: "graphite",
    paths: (
      <>
        <path d="M6 3h9l4 4v14H6z" />
        <path d="M15 3v4h4" />
      </>
    ),
  },
  video: {
    tone: "pink",
    paths: (
      <>
        <rect x="3" y="6" width="13" height="12" rx="2" />
        <path d="M16 10l5-3v10l-5-3" />
      </>
    ),
  },
  archive: {
    tone: "amber",
    paths: (
      <>
        <rect x="3" y="4" width="18" height="5" rx="1.5" />
        <path d="M5 9v10h14V9M10 13h4" />
      </>
    ),
  },
  folder: { tone: "graphite", paths: folder },
};

/** CategoryIcon is the 42px tile of a category, from its Icon key ("cache", "log", "crash", "temp", "trash", "browser", "mail", "update", "xcode", "simulator", "node", "package", "docker", "build", "installer", "disk", "app", "leftover", "file", "video", "archive", "folder"). */
export function CategoryIcon({ icon }: { icon: string }) {
  const glyph = GLYPHS[icon] ?? { tone: "graphite" as const, paths: folder };
  return (
    <IconTile tone={glyph.tone} size={42} glyphSize={20} className="category-icon">
      {glyph.paths}
    </IconTile>
  );
}

/** CATEGORY_ICONS lists the keys CategoryIcon draws, for tests and for engine authors. */
export const CATEGORY_ICONS: readonly string[] = Object.keys(GLYPHS);
