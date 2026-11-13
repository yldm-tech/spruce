import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { useShellBlocked } from "./ShellBlock";

/** A sidebar entry. Entries without `to` are features of later milestones: they render disabled with the 即将推出 tooltip. */
interface NavEntry {
  label: string;
  icon: ReactNode;
  to?: "/maintenance";
}

interface NavGroup {
  label?: string;
  entries: NavEntry[];
}

const COMING_SOON = "即将推出";

function Tile({ gradient, children }: { gradient: string; children: ReactNode }) {
  return (
    <span aria-hidden="true" className="nav-icon" style={{ background: `var(${gradient})` }}>
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="#FFFFFF"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {children}
      </svg>
    </span>
  );
}

function FooterGlyph({ children }: { children: ReactNode }) {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.9"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

const GROUPS: NavGroup[] = [
  {
    entries: [
      {
        label: "智能扫描",
        icon: (
          <Tile gradient="--gradient-violet">
            <circle cx="12" cy="12" r="8" />
            <path d="M12 4v4M12 16v4M4 12h4M16 12h4" />
          </Tile>
        ),
      },
    ],
  },
  {
    label: "清理",
    entries: [
      {
        label: "系统垃圾",
        icon: (
          <Tile gradient="--gradient-green">
            <path d="M14 3l-3 8" />
            <path d="M5 12h12l1.5 8h-15z" />
          </Tile>
        ),
      },
      {
        label: "开发者垃圾",
        icon: (
          <Tile gradient="--gradient-orange">
            <path d="M8 7l-5 5 5 5M16 7l5 5-5 5" />
          </Tile>
        ),
      },
      {
        label: "安装包",
        icon: (
          <Tile gradient="--gradient-pink">
            <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />
          </Tile>
        ),
      },
    ],
  },
  {
    label: "应用与空间",
    entries: [
      {
        label: "应用卸载",
        icon: (
          <Tile gradient="--gradient-blue">
            <rect x="4" y="4" width="7" height="7" rx="2" />
            <rect x="13" y="4" width="7" height="7" rx="2" />
            <rect x="4" y="13" width="7" height="7" rx="2" />
            <path d="M14 16.5h5" />
          </Tile>
        ),
      },
      {
        label: "空间透视",
        icon: (
          <Tile gradient="--gradient-cyan">
            <path d="M12 3a9 9 0 1 0 9 9h-9z" />
          </Tile>
        ),
      },
    ],
  },
  {
    label: "性能",
    entries: [
      {
        label: "维护",
        to: "/maintenance",
        icon: (
          <Tile gradient="--gradient-amber">
            <path d="M13 3L5 14h6l-1 7 8-11h-6z" />
          </Tile>
        ),
      },
      {
        label: "健康监测",
        icon: (
          <Tile gradient="--gradient-red">
            <path d="M3 12h4l3-7 4 14 3-7h4" />
          </Tile>
        ),
      },
    ],
  },
];

const FOOTER: NavEntry[] = [
  {
    label: "操作历史",
    icon: (
      <FooterGlyph>
        <path d="M3.5 12a8.5 8.5 0 1 0 2.5-6" />
        <path d="M3 4v4h4" />
        <path d="M12 8v4l3 2" />
      </FooterGlyph>
    ),
  },
  {
    label: "设置",
    icon: (
      <FooterGlyph>
        <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
        <circle cx="15" cy="7" r="2" />
        <circle cx="9" cy="17" r="2" />
      </FooterGlyph>
    ),
  },
];

function NavItem({ entry, footer }: { entry: NavEntry; footer?: boolean }) {
  const className = footer ? "nav-item nav-item--footer" : "nav-item";
  if (entry.to) {
    return (
      <Link to={entry.to} className={className}>
        {entry.icon}
        {entry.label}
      </Link>
    );
  }
  // A disabled link stays focusable, so keyboard and screen reader users still find the entry and hear that it is unavailable.
  return (
    <span role="link" tabIndex={0} className={className} aria-disabled="true" title={COMING_SOON}>
      {entry.icon}
      {entry.label}
    </span>
  );
}

/** The app's sidebar, as designed: every section is listed, only 维护 is built in this milestone. It is inert while a page's modal overlay is up. */
export function Sidebar() {
  const blocked = useShellBlocked();
  return (
    <nav aria-label="Spruce" className="sidebar" inert={blocked}>
      <div className="sidebar-titlebar" />
      {GROUPS.map((group, i) => (
        <div key={group.label ?? i} className="sidebar-group">
          {group.label && <span className="sidebar-group-label">{group.label}</span>}
          {group.entries.map((entry) => (
            <NavItem key={entry.label} entry={entry} />
          ))}
        </div>
      ))}
      <div className="sidebar-footer">
        {FOOTER.map((entry) => (
          <NavItem key={entry.label} entry={entry} footer />
        ))}
      </div>
    </nav>
  );
}
