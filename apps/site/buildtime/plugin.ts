// The Vite plugins that build the site from the repository: spruceSite collects the data (collect.ts) and serves it to the pages as virtual modules, and sprucePostBuild finishes the prerendered HTML.
import { existsSync, readdirSync } from "node:fs";
import { join, resolve } from "node:path";
import type { Plugin } from "vite";
import { langCodes, pagePath, type LangCode } from "../src/lib/site";
import { collect, type Collected } from "./collect";
import { finishHTML } from "./csp";

export const siteModule = "virtual:spruce/site";
export const contentPrefix = "virtual:spruce/content/";
// loadersModule is the map of each built language's content module, imported lazily, so that a page loads only its own language and a language without a content file is left out.
export const loadersModule = "virtual:spruce/content-loaders";

export interface SpruceSiteOptions {
  // repoRoot is the repository root, relative to the site's folder.
  repoRoot: string;
  // outDir is the build's output folder, relative to the site's folder; the pages are in its client folder.
  outDir: string;
  // publicDir is the folder the collected files are written to, served and copied as Vite's public folder.
  publicDir: string;
  // contentDir is the folder of the content files, relative to the site's folder.
  contentDir: string;
}

// checkOutDir refuses an output folder that holds anything but a previous build (one with client/CNAME), so that a mistyped outDir cannot wipe a folder of other files when the build empties it.
export function checkOutDir(out: string): void {
  if (!existsSync(out)) {
    return;
  }
  if (readdirSync(out).length > 0 && !existsSync(join(out, "client", "CNAME"))) {
    throw new Error(`${out} is not empty and holds no previous build (no client/CNAME); choose another output folder`);
  }
}

// collected is the data of this process, collected once: Vite loads the config again for the prerender's preview server, and the files are the same.
const cacheKey = Symbol.for("spruce-site.collected");
function collectOnce(root: string, siteDir: string, contentDir: string, pub: string): Collected {
  const g = globalThis as { [cacheKey]?: Collected };
  g[cacheKey] ??= collect(root, siteDir, contentDir, pub);
  return g[cacheKey];
}

// builtCodes are the languages with a content file in contentDir, in the switcher's order; the build fails later, in collect, when the source language has none.
export function builtCodes(contentDir: string): LangCode[] {
  return langCodes.filter((code) => existsSync(join(contentDir, code + ".json")));
}

// prerenderPages are the pages TanStack Start renders to static HTML: the page of each language with a content file in contentDir, and 404.html.
export function prerenderPages(contentDir: string): { path: string; prerender?: { outputPath: string } }[] {
  return [...builtCodes(contentDir).map((code) => ({ path: pagePath(code) })), { path: "/404", prerender: { outputPath: "/404.html" } }];
}

// loadersSource is the code of the loaders module for the languages codes.
export function loadersSource(codes: readonly LangCode[]): string {
  const entries = codes.map((code) => `  ${JSON.stringify(code)}: () => import(${JSON.stringify(contentPrefix + code)}),`);
  return `export default {\n${entries.join("\n")}\n};\n`;
}

// The client build's chunk of each language's content module, by language code, which sprucePostBuild preloads on that language's page. Kept on globalThis, as the client build and the post-build step see different copies of this file when Vite loads the config again.
const chunksKey = Symbol.for("spruce-site.content-chunks");
function contentChunks(): Map<LangCode, string> {
  const g = globalThis as { [chunksKey]?: Map<LangCode, string> };
  g[chunksKey] ??= new Map();
  return g[chunksKey];
}

export function spruceSite(opts: SpruceSiteOptions): Plugin {
  let data: Collected | undefined;
  const need = (): Collected => {
    if (!data) {
      throw new Error("spruce-site: the data was not collected");
    }
    return data;
  };
  return {
    name: "spruce-site",
    enforce: "pre",
    config(_config, env) {
      const siteDir = process.cwd();
      if (env.command === "build") {
        checkOutDir(resolve(siteDir, opts.outDir));
      }
      data = collectOnce(resolve(siteDir, opts.repoRoot), siteDir, resolve(siteDir, opts.contentDir), resolve(siteDir, opts.publicDir));
    },
    resolveId(id) {
      if (id === siteModule || id === loadersModule || id.startsWith(contentPrefix)) {
        return "\0" + id;
      }
      return undefined;
    },
    load(id) {
      if (!id.startsWith("\0virtual:spruce/")) {
        return undefined;
      }
      const name = id.slice(1);
      const json = (v: unknown) => `export default JSON.parse(${JSON.stringify(JSON.stringify(v))});\n`;
      if (name === siteModule) {
        return json(need().site);
      }
      if (name === loadersModule) {
        return loadersSource([...need().content.keys()]);
      }
      const code = name.slice(contentPrefix.length) as LangCode;
      const content = need().content.get(code);
      if (!content) {
        throw new Error(`spruce-site: no content for ${JSON.stringify(code)}`);
      }
      return json(content);
    },
    generateBundle(_options, bundle) {
      if (this.environment.name !== "client") {
        return;
      }
      for (const chunk of Object.values(bundle)) {
        const id = chunk.type === "chunk" ? (chunk.facadeModuleId ?? "") : "";
        const at = id.indexOf(contentPrefix);
        if (at >= 0) {
          contentChunks().set(id.slice(at + contentPrefix.length) as LangCode, "/" + chunk.fileName);
        }
      }
    },
  };
}

// sprucePostBuild runs after TanStack Start has prerendered the pages: it preloads each page's content chunk, puts in each page's structured data, gives each page its Content Security Policy and takes the scripts out of 404.html (finishHTML).
export function sprucePostBuild(opts: { outDir: string }): Plugin {
  return {
    name: "spruce-site-post-build",
    enforce: "post",
    apply: "build",
    buildApp: {
      order: "post",
      handler() {
        const data = (globalThis as { [cacheKey]?: Collected })[cacheKey];
        if (!data) {
          throw new Error("spruce-site: the data was not collected");
        }
        finishHTML(resolve(process.cwd(), opts.outDir, "client"), contentChunks(), data.structured);
        return Promise.resolve();
      },
    },
  };
}
