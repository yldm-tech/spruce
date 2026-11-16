// collect reads everything the site shows from the repository, checks it, and writes the files the pages link into the public folder: the static script, the icons, the menu bar icons, the screenshots and link preview images, the sitemap, robots.txt, CNAME and the web app manifest; and it makes each page's structured data. Any source that is missing or changed shape fails the build.
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { minifySync } from "vite";
import { languages, pagePath, shotNames, sourceLang, type LangCode, type ShotName } from "../src/lib/site";
import { jsonForScript } from "../src/lib/json";
import { appVersion } from "./app";
import { loadContent, loadNotes } from "./content";
import { pngSize, writeIcons } from "./icons";
import { structuredData } from "./jsonld";
import { hashLink, iconFile, mediaCommand, mediaDir, ogFile, ogSize, shotFile, shotFormats, shotPNG, shotWidths } from "./media";
import { siteDataSchema, type ContentModule, type Picture, type SiteData } from "./schema";
import { lastModified, writeSEOFiles } from "./seo";

export interface Collected {
  site: SiteData;
  content: Map<LangCode, ContentModule>;
  // structured is each page's JSON-LD (buildtime/jsonld.ts), by the page's path.
  structured: Map<string, string>;
}

// trayIcons are the menu bar icons the page shows beside the menu bar's paragraph, relative to the repository root: template images, black on transparent, which the stylesheet turns white.
export const trayIcons = { normal: "apps/spruce/assets/tray.png", alert: "apps/spruce/assets/tray-alert.png" } as const;

// collect builds the data from the repository at root, for the site at siteDir with its content files in contentDir, writing its files into pub, which it empties first. A language has pages only when it has a content file.
export function collect(root: string, siteDir: string, contentDir: string, pub: string): Collected {
  const content = loadContent(contentDir);
  const en = content.get(sourceLang);
  if (!en) {
    throw new Error("content/en.json is missing");
  }
  loadNotes(contentDir, en);
  const langs = languages.filter((l) => content.has(l.code));

  rmSync(pub, { recursive: true, force: true });
  mkdirSync(join(pub, "static"), { recursive: true });

  // The static script is not bundled, so it is minified here as Vite minifies the bundles: site.js blocks the first paint, and the smaller it is the sooner the page shows.
  const min = minifySync("site.js", readFileSync(join(siteDir, "static", "site.js"), "utf8"));
  if (min.errors.length > 0) {
    throw new Error(`static/site.js: ${min.errors.map((e) => e.message).join("; ")}`);
  }
  writeFileSync(join(pub, "static", "site.js"), min.code);
  const siteJS = hashLink("/static/site.js", min.code);

  writeIcons(join(root, iconFile), pub);

  // copyFile copies the file src of the repository to to in the site, and links it with a hash of its content.
  const copyFile = (src: string, to: string): string => {
    const data = readFileSync(join(root, src));
    mkdirSync(dirname(join(pub, to)), { recursive: true });
    writeFileSync(join(pub, to), data);
    return hashLink("/" + to, data);
  };
  const tray = {
    normal: { src: copyFile(trayIcons.normal, "tray.png"), ...pngSize(join(root, trayIcons.normal)) },
    alert: { src: copyFile(trayIcons.alert, "tray-alert.png"), ...pngSize(join(root, trayIcons.alert)) },
  };

  // The images scripts/media.ts makes (buildtime/media.ts) are copied from media/, linked with a hash of their content so that a new one is fetched again. A missing one fails the build, except in the build the script makes for itself before it has made them.
  const pending = process.env["SPRUCE_SITE_MEDIA_PENDING"] === "1";
  const copyMedia = (rel: string): string => {
    const src = join(siteDir, mediaDir, rel);
    if (!existsSync(src)) {
      if (pending) {
        return "/" + rel;
      }
      throw new Error(`${mediaDir}/${rel} is missing; run ${mediaCommand} in apps/site`);
    }
    const data = readFileSync(src);
    mkdirSync(dirname(join(pub, rel)), { recursive: true });
    writeFileSync(join(pub, rel), data);
    return hashLink("/" + rel, data);
  };

  const shots = {} as Record<ShotName, Picture>;
  for (const name of shotNames) {
    const png = shotPNG(name);
    const size = pngSize(join(root, png));
    const sources = shotFormats.map((format) => ({
      type: "image/" + format,
      srcset: shotWidths.map((w) => `${copyMedia(shotFile(name, w, format))} ${w}w`).join(", "),
    }));
    shots[name] = { src: copyFile(png, `shots/${name}.png`), ...size, sources };
  }

  const og = {} as SiteData["og"];
  for (const l of langs) {
    const card = ogFile(l.code);
    if (existsSync(join(siteDir, mediaDir, card))) {
      const got = pngSize(join(siteDir, mediaDir, card));
      if (got.w !== ogSize.w || got.h !== ogSize.h) {
        throw new Error(`${mediaDir}/${card} is ${got.w}×${got.h}, not ${ogSize.w}×${ogSize.h}; run ${mediaCommand} in apps/site`);
      }
    }
    og[l.code] = { src: copyMedia(card), w: ogSize.w, h: ogSize.h };
  }

  const version = appVersion(root);
  const modules = new Map<LangCode, ContentModule>();
  for (const l of langs) {
    modules.set(l.code, { code: l.code, strings: content.get(l.code) ?? {} });
  }

  const site = siteDataSchema.parse({
    version,
    shots,
    og,
    tray,
    assets: { siteJS },
    missing: langs.map((l) => {
      const s = content.get(l.code) ?? {};
      return { code: l.code, tag: l.tag, title: s["notFoundTitle"], body: s["notFoundBody"], home: s["notFoundHome"] };
    }),
  });

  writeSEOFiles(pub, langs, { lastmod: lastModified(root), description: en["metaDescription"] ?? "" });

  // The structured data of each page, by its path, which the post-build step puts into the page (buildtime/csp.ts).
  const structured = new Map<string, string>();
  for (const l of langs) {
    const graph = structuredData({ code: l.code, strings: content.get(l.code) ?? {}, version, shot: shots["overview-ready"] });
    structured.set(pagePath(l.code), jsonForScript(graph));
  }
  return { site, content: modules, structured };
}
