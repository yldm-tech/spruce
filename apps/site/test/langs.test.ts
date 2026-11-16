// A language has pages only when it has a content file. The site is built again from a copy of the content folder without one language that is not the source, and that language is left out of the pages, the language switcher and the sitemap while the others are built as before.
import { spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { afterAll, beforeAll, expect, it } from "vite-plus/test";
import { builtCodes, loadersSource, prerenderPages } from "../buildtime/plugin";
import { siteDir } from "./helpers";

const dropped = "ko";
let tmp = "";
let pages = "";

beforeAll(() => {
  mkdirSync(join(siteDir, ".generated"), { recursive: true });
  tmp = mkdtempSync(join(resolve(siteDir, ".generated"), "test-langs-"));
  const content = join(tmp, "content");
  cpSync(join(siteDir, "content"), content, { recursive: true });
  rmSync(join(content, dropped + ".json"));
  const out = join(tmp, "out");
  const build = spawnSync("vp", ["build"], {
    cwd: siteDir,
    env: { ...process.env, NODE_ENV: "production", SPRUCE_SITE_OUT: out, SPRUCE_SITE_CONTENT: content, SPRUCE_SITE_PUBLIC: join(tmp, "public") },
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 280_000,
  });
  if (build.status !== 0) {
    throw new Error(`vp build without ${dropped}.json failed (${build.status ?? build.signal}):\n${build.stdout}\n${build.stderr}`);
  }
  pages = join(out, "client");
});

afterAll(() => {
  if (tmp) {
    rmSync(tmp, { recursive: true, force: true });
  }
});

it("lists only the languages with a content file to prerender and to load", () => {
  const content = join(tmp, "content");
  expect(builtCodes(content)).toEqual(["en", "zh-CN", "zh-TW", "ja"]);
  const paths = prerenderPages(content).map((p) => p.path);
  expect(paths).toEqual(["/", "/zh-CN/", "/zh-TW/", "/ja/", "/404"]);
  expect(loadersSource(builtCodes(content))).not.toContain(`content/${dropped}`);
});

it("builds the other languages and leaves the one without content out", () => {
  for (const path of ["index.html", "zh-CN/index.html", "zh-TW/index.html", "ja/index.html", "404.html", "og/ja.png"]) {
    expect(existsSync(join(pages, path)), path).toBe(true);
  }
  for (const path of [`${dropped}/index.html`, `og/${dropped}.png`]) {
    expect(existsSync(join(pages, path)), path).toBe(false);
  }
  const home = readFileSync(join(pages, "index.html"), "utf8");
  expect(home).not.toContain(`href="/${dropped}/"`);
  expect(home).not.toMatch(/hreflang="ko"/i);
  expect(home).toContain('href="/ja/"');
  expect(readFileSync(join(pages, "sitemap.xml"), "utf8")).not.toContain(`/${dropped}/`);
  expect(readFileSync(join(pages, "404.html"), "utf8")).not.toContain(`lang="ko"`);
});
