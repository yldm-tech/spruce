// The sitemap lists each page with all of its alternates and the date it last changed, robots.txt and CNAME point at the site's address, and the web app manifest names the app and its icons.
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vite-plus/test";
import { pngSize } from "../buildtime/icons";
import { lastModified, lastmodPattern } from "../buildtime/seo";
import { pageURL, siteURL, sourceLang, themeColor } from "../src/lib/site";
import { builtLangs, dist, readJSON, readOut, siteDir } from "./helpers";

it("lists every page in the sitemap with its alternates and when it last changed", () => {
  const sitemap = readOut("sitemap.xml");
  const langs = builtLangs();
  expect((sitemap.match(/<loc>/g) ?? []).length).toBe(langs.length);
  for (const l of langs) {
    expect(sitemap).toContain(`<loc>${pageURL(l.code)}</loc>`);
    const alt = `<xhtml:link rel="alternate" hreflang="${l.tag}" href="${pageURL(l.code)}"/>`;
    expect(sitemap.split(alt).length - 1, alt).toBe(langs.length);
  }
  const xDefault = `hreflang="x-default" href="${pageURL(sourceLang)}"`;
  expect(sitemap.split(xDefault).length - 1).toBe(langs.length);
  for (const url of sitemap.split("<url>").slice(1)) {
    const lastmod = url.match(/<lastmod>([^<]*)<\/lastmod>/)?.[1] ?? "";
    expect(lastmod, url).toMatch(lastmodPattern);
    expect(Date.parse(lastmod)).toBeLessThanOrEqual(Date.now());
  }
});

it("writes CNAME and robots.txt", () => {
  expect(readOut("CNAME")).toBe("spruce.yldm.ai\n");
  expect(readOut("robots.txt")).toBe(`User-agent: *\nAllow: /\n\nSitemap: ${siteURL}/sitemap.xml\n`);
});

it("writes a web app manifest with the app's name, colors and icons", () => {
  const manifest = JSON.parse(readOut("site.webmanifest")) as Record<string, unknown> & { icons: { src: string; sizes: string; type: string }[] };
  expect(manifest["name"]).toBe("Spruce");
  expect(manifest["short_name"]).toBe("Spruce");
  expect(manifest["start_url"]).toBe("/");
  expect(manifest["display"]).toBe("standalone");
  expect(manifest["theme_color"]).toBe(themeColor);
  expect(manifest["background_color"]).toBe(themeColor);
  expect(manifest["description"]).toBe(readJSON(join(siteDir, "content", "en.json"))["metaDescription"]);
  expect(manifest.icons.map((i) => i.sizes).sort()).toEqual(["192x192", "512x512"]);
  for (const icon of manifest.icons) {
    expect(icon.type).toBe("image/png");
    const side = Number(icon.sizes.split("x")[0]);
    expect(pngSize(join(dist(), icon.src)), icon.src).toEqual({ w: side, h: side });
  }
});

// A commit made just after midnight in Tokyo is still the day before in UTC: the sitemap's time keeps the commit's offset, so it names that instant rather than a date that reads as midnight UTC of the next day, ahead of the moment the site is built.
it("dates the sitemap with the commit's time and offset", () => {
  const repo = mkdtempSync(join(tmpdir(), "spruce-lastmod-"));
  try {
    const git = (...args: string[]) => {
      const r = spawnSync("git", ["-C", repo, ...args], {
        encoding: "utf8",
        env: { ...process.env, GIT_AUTHOR_DATE: "2026-11-11T00:33:26+09:00", GIT_COMMITTER_DATE: "2026-11-11T00:33:26+09:00" },
      });
      expect(r.status, r.stderr).toBe(0);
    };
    git("init", "-q");
    git("-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "c");
    const lastmod = lastModified(repo);
    expect(lastmod).toMatch(lastmodPattern);
    expect(new Date(lastmod).toISOString()).toBe("2026-11-10T15:33:26.000Z");
  } finally {
    rmSync(repo, { recursive: true, force: true });
  }
});
