import { IconTile } from "../common/Glyph";

/** The generic app glyph, for apps whose icon could not be read (macOS apps with only an asset catalog, Windows programs without an icon). */
const GENERIC = (
  <>
    <rect x="5" y="5" width="14" height="14" rx="4" />
    <path d="M9 12h6" />
  </>
);

/** AppIcon is an app's icon at size pixels: its own (a data: URL from Uninstall.measure) or the generic tile. Decorative: the name is next to it. */
export function AppIcon({ icon, size }: { icon?: string; size: number }) {
  if (icon) {
    return <img src={icon} alt="" aria-hidden="true" width={size} height={size} className="uninstall-icon" />;
  }
  return (
    <IconTile tone="blue" size={size} glyphSize={Math.round(size * 0.55)} className="uninstall-icon-tile">
      {GENERIC}
    </IconTile>
  );
}
