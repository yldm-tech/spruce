import type { ReactNode } from "react";

interface GlyphProps {
  size: number;
  strokeWidth?: number;
  /** The stroke colour; white on gradient tiles. */
  color?: string;
  children: ReactNode;
}

/** Glyph draws the app's line icons: a 24-unit grid with round caps and joins, decorative (hidden from assistive technology). */
export function Glyph({ size, strokeWidth = 2, color = "#FFFFFF", children }: GlyphProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke={color}
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

/** A tile gradient of tokens.css (--gradient-<tone>, with its --glow-<tone>). */
export type Tone =
  | "violet"
  | "green"
  | "orange"
  | "pink"
  | "blue"
  | "cyan"
  | "amber"
  | "red"
  | "slate"
  | "graphite";

/** IconTile is a gradient tile holding a white glyph, as on the sidebar, the page headers and the cards. */
export function IconTile({
  tone,
  size,
  glyphSize,
  className,
  glow,
  strokeWidth,
  children,
}: {
  tone: Tone;
  size: number;
  glyphSize: number;
  /** Sets the tile's corner radius and placement; see common.css. */
  className?: string;
  strokeWidth?: number;
  /** Adds the tone's glow under the tile, as the page header does. */
  glow?: boolean;
  children: ReactNode;
}) {
  return (
    <span
      aria-hidden="true"
      className={className ? `icon-tile ${className}` : "icon-tile"}
      style={{
        width: size,
        height: size,
        background: `var(--gradient-${tone})`,
        boxShadow: glow ? `0 10px 24px var(--glow-${tone})` : undefined,
      }}
    >
      <Glyph size={glyphSize} strokeWidth={strokeWidth}>
        {children}
      </Glyph>
    </span>
  );
}
