import { Link } from "@tanstack/react-router";

import { Glyph, IconTile } from "./common/Glyph";
import { PAGES, type PageId } from "./common/pages";
import { useShellBlocked } from "./ShellBlock";

/** The route of every sidebar entry. */
type NavPath = `/${PageId}`;

interface NavGroup {
  label?: string;
  pages: PageId[];
}

/** The sidebar as designed: the scan, the cleaners, apps and space, performance, and the footer. */
const GROUPS: NavGroup[] = [
  { pages: ["smart-scan"] },
  { label: "清理", pages: ["system-junk", "dev-junk", "installers"] },
  { label: "应用与空间", pages: ["uninstall", "space"] },
  { label: "性能", pages: ["maintenance", "health"] },
];

const FOOTER: PageId[] = ["history", "settings"];

function NavItem({ page, footer }: { page: PageId; footer?: boolean }) {
  const meta = PAGES[page];
  const to: NavPath = `/${page}`;
  return (
    <Link to={to} className={footer ? "nav-item nav-item--footer" : "nav-item"}>
      {footer ? (
        <Glyph size={18} strokeWidth={1.9} color="currentColor">
          {meta.icon}
        </Glyph>
      ) : (
        <IconTile tone={meta.tone} size={26} glyphSize={15} strokeWidth={2.2} className="nav-icon">
          {meta.icon}
        </IconTile>
      )}
      {meta.title}
    </Link>
  );
}

/** The app's sidebar: one link per page, the current one marked. It is inert while a page's modal overlay is up. */
export function Sidebar() {
  const blocked = useShellBlocked();
  return (
    <nav aria-label="Spruce" className="sidebar" inert={blocked}>
      <div className="sidebar-titlebar" />
      {GROUPS.map((group, i) => (
        <div key={group.label ?? i} className="sidebar-group">
          {group.label && <span className="sidebar-group-label">{group.label}</span>}
          {group.pages.map((page) => (
            <NavItem key={page} page={page} />
          ))}
        </div>
      ))}
      <div className="sidebar-footer">
        {FOOTER.map((page) => (
          <NavItem key={page} page={page} footer />
        ))}
      </div>
    </nav>
  );
}
