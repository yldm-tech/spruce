import type { ReactNode } from "react";

import { useShellBlocked } from "../ShellBlock";
import { IconTile, type Tone } from "./Glyph";
import type { PageMeta } from "./pages";

import "./common.css";

interface PageProps {
  className?: string;
  /** Makes the page inert. By default it is inert while any overlay blocks the shell, which updates one render after the overlay goes; a page that moves focus into itself in the same render that closes its overlay passes its own state here. */
  blocked?: boolean;
  children: ReactNode;
}

/** Page is a page's scroll container. It turns inert while an overlay is up, so focus and assistive technology stay in the overlay; overlays are drawn outside it (see OverlayHost). */
export function Page({ className, blocked, children }: PageProps) {
  const shellBlocked = useShellBlocked();
  return (
    <div className={className ? `page ${className}` : "page"} inert={blocked ?? shellBlocked}>
      {children}
    </div>
  );
}

interface PageHeaderProps {
  title: string;
  subtitle: string;
  tone: Tone;
  /** The glyph's paths on the 24-unit grid. */
  icon: ReactNode;
  /** Controls on the right of the header, such as a 重新扫描 button. */
  children?: ReactNode;
}

/** PageHeader is the top of every page, as the 维护 design draws it: a 48px gradient tile with its glow, the title and the subtitle. It reaches the top of the window and drags it. Spread a PAGES entry into it: `<PageHeader {...PAGES["system-junk"]} />`. */
export function PageHeader({ title, subtitle, tone, icon, children }: PageHeaderProps) {
  return (
    <header className="page-header">
      <div className="page-header-title">
        <IconTile tone={tone} size={48} glyphSize={24} className="page-header-icon" glow>
          {icon}
        </IconTile>
        <div className="page-header-text">
          <h1>{title}</h1>
          <span>{subtitle}</span>
        </div>
      </div>
      {children && <div className="page-header-actions">{children}</div>}
    </header>
  );
}

/** PageShell is a page that so far shows only its header. */
export function PageShell({ meta }: { meta: PageMeta }) {
  return (
    <Page>
      <PageHeader {...meta} />
    </Page>
  );
}
