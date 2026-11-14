import type { ReactNode } from "react";

import type { Tone } from "./Glyph";

/** The app's pages, by route path without the slash. */
export type PageId =
  | "smart-scan"
  | "system-junk"
  | "dev-junk"
  | "installers"
  | "uninstall"
  | "space"
  | "maintenance"
  | "health"
  | "history"
  | "settings";

/** What the sidebar and a page's header show for a page. */
export interface PageMeta {
  title: string;
  subtitle: string;
  tone: Tone;
  /** The glyph's paths on the 24-unit grid. */
  icon: ReactNode;
}

/** PAGES is the one place each page's name, subtitle, tile colour and glyph are defined: the sidebar entry and the page header both read it. */
export const PAGES: Record<PageId, PageMeta> = {
  "smart-scan": {
    title: "智能扫描",
    subtitle: "一次扫描系统垃圾、开发者垃圾和安装包，按推荐勾选可以放心清理的",
    tone: "violet",
    icon: (
      <>
        <circle cx="12" cy="12" r="8" />
        <path d="M12 4v4M12 16v4M4 12h4M16 12h4" />
      </>
    ),
  },
  "system-junk": {
    title: "系统垃圾",
    subtitle: "清理应用缓存、日志和临时文件，不会动你的文档和照片",
    tone: "green",
    icon: (
      <>
        <path d="M14 3l-3 8" />
        <path d="M5 12h12l1.5 8h-15z" />
      </>
    ),
  },
  "dev-junk": {
    title: "开发者垃圾",
    subtitle: "清理开发工具的缓存和项目里的构建产物，大多会在需要时重新下载或生成",
    tone: "orange",
    icon: <path d="M8 7l-5 5 5 5M16 7l5 5-5 5" />,
  },
  installers: {
    title: "安装包",
    subtitle: "找出装完就没再用的安装包和磁盘映像",
    tone: "pink",
    icon: <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />,
  },
  uninstall: {
    title: "应用卸载",
    subtitle: "连同应用留下的缓存和设置一起移除",
    tone: "blue",
    icon: (
      <>
        <rect x="4" y="4" width="7" height="7" rx="2" />
        <rect x="13" y="4" width="7" height="7" rx="2" />
        <rect x="4" y="13" width="7" height="7" rx="2" />
        <path d="M14 16.5h5" />
      </>
    ),
  },
  space: {
    title: "空间透视",
    subtitle: "看看磁盘空间都被什么占用了",
    tone: "cyan",
    icon: <path d="M12 3a9 9 0 1 0 9 9h-9z" />,
  },
  maintenance: {
    title: "维护",
    subtitle: "给系统做一次轻量保养，不会动你的文档和照片",
    tone: "amber",
    icon: <path d="M13 3L5 14h6l-1 7 8-11h-6z" />,
  },
  health: {
    title: "健康监测",
    subtitle: "实时查看 CPU、内存、磁盘、网络和电池，找出最占资源的进程",
    tone: "red",
    icon: <path d="M3 12h4l3-7 4 14 3-7h4" />,
  },
  history: {
    title: "操作历史",
    subtitle: "Spruce 做过的每一次维护、清理和卸载",
    tone: "slate",
    icon: (
      <>
        <path d="M3.5 12a8.5 8.5 0 1 0 2.5-6" />
        <path d="M3 4v4h4" />
        <path d="M12 8v4l3 2" />
      </>
    ),
  },
  settings: {
    title: "设置",
    subtitle: "调整提醒、清理范围、排除的文件夹和历史记录",
    tone: "graphite",
    icon: (
      <>
        <path d="M4 7h9M17 7h3M4 17h3M11 17h9" />
        <circle cx="15" cy="7" r="2" />
        <circle cx="9" cy="17" r="2" />
      </>
    ),
  },
};
