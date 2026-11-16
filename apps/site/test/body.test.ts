// The page body: each page has its sections in order, its images keep their space as they load and say what they show, every screenshot is offered as AVIF and WebP before its PNG, and the Download buttons fetch the files of the version in apps/spruce/nsgo.yaml.
import { existsSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { appVersion } from "../buildtime/app";
import { pngSize } from "../buildtime/icons";
import { shotFormats, shotPNG, shotWidths } from "../buildtime/media";
import { macInstallCommand, readmeURL, releasesURL, shotNames, siteURL, windowsInstallCommand } from "../src/lib/site";
import { attr, builtLangs, content, decode, dist, pageFile, readOut, repoRoot, section, tags, text } from "./helpers";

// sections are the page's sections by id, in the page's order.
const sections = ["how", "features", "developers", "details", "safety", "privacy", "download", "faq"];

describe("page body", () => {
  const version = appVersion(repoRoot);
  for (const l of builtLangs()) {
    it(l.code, () => {
      const html = readOut(pageFile(l));
      const at = sections.map((id) => html.indexOf(` id="${id}"`));
      for (const [i, pos] of at.entries()) {
        expect(pos, `no #${sections[i]}`).toBeGreaterThan(0);
      }
      expect(at).toEqual([...at].sort((a, b) => a - b));

      for (const img of tags(html, "img")) {
        expect(attr(img, "width") && attr(img, "height"), `image without width and height, which shifts the layout as it loads: ${img.raw}`).toBeTruthy();
        expect(img.attrs.has("alt"), `image without alt: ${img.raw}`).toBe(true);
      }

      // Every screenshot is on the page once, with its alt text in the page's language and its size; its smaller formats are files of the site in the widths their srcset names.
      const strings = content(l);
      for (const name of shotNames) {
        const pictures = html
          .split("<picture>")
          .slice(1)
          .filter((p) => p.includes(`/shots/${name}.png`));
        expect(pictures.length, name).toBe(1);
        const picture = (pictures[0] ?? "").slice(0, (pictures[0] ?? "").indexOf("</picture>"));
        const img = tags(picture, "img")[0];
        const size = pngSize(join(repoRoot, shotPNG(name)));
        expect(img && [attr(img, "width"), attr(img, "height")], name).toEqual([String(size.w), String(size.h)]);
        expect(img && attr(img, "loading"), name).toBe("lazy");
        expect(Object.values(strings), `${name}'s alt text is a content string`).toContain(img && attr(img, "alt"));
        const sources = tags(picture, "source");
        expect(sources.map((s) => attr(s, "type"))).toEqual(shotFormats.map((f) => "image/" + f));
        for (const s of sources) {
          expect(attr(s, "sizes")).toBeTruthy();
          const candidates = attr(s, "srcset")
            .split(",")
            .map((c) => c.trim().split(/\s+/));
          expect(candidates.map((c) => c[1])).toEqual(shotWidths.map((w) => w + "w"));
          for (const [ref] of candidates) {
            expect(existsSync(join(dist(), new URL(ref ?? "", siteURL).pathname)), ref).toBe(true);
          }
        }
      }

      // The Download buttons fetch this release's files rather than opening its page: the hero's and the closing one the disk image for Apple silicon, and each tab of the download section its system's files, one per architecture, then the checksums.
      const files = `${releasesURL}/download/v${version}/`;
      const downloads = tags(html, "a")
        .map((t) => attr(t, "href"))
        .filter((href) => href.startsWith(files))
        .map((href) => href.slice(files.length));
      const mac = `spruce-${version}-macos-arm64.dmg`;
      expect(downloads).toEqual([
        mac,
        mac,
        `spruce-${version}-macos-x64.dmg`,
        `spruce-${version}-windows-x64-setup.exe`,
        `spruce-${version}-windows-arm64-setup.exe`,
        "checksums.txt",
        mac,
      ]);
      expect(tags(html, "a").some((t) => attr(t, "href").endsWith("/releases/latest"))).toBe(false);

      // The Windows tab says its installers are not code-signed, and each tab has its install command.
      const win = section(html, 'id="panel-win"', "</section>");
      expect(text(win)).toContain(strings["dlWinUnsigned"]);
      const commands = [...html.matchAll(/<div class="code"><pre><code>([\s\S]*?)<\/code><\/pre>/g)].map((m) => text(m[1] ?? "").replace(/^(\$|PS>) /, ""));
      expect(commands).toEqual([macInstallCommand, windowsInstallCommand]);

      // The interface language is named on every page but the Simplified Chinese one, which is in that language. Only the page's main content counts: the router state after it holds every string of the language.
      expect(text(section(html, "<main", "</main>")).includes(strings["dlLang"] ?? ""), "dlLang").toBe(l.code !== "zh-CN");

      expect(tags(html, "a").some((t) => decode(attr(t, "href")).startsWith(readmeURL(l) + "#" + l.cliSection))).toBe(true);
      const foot = section(html, 'class="foot-links"', "</nav>");
      expect(tags(foot, "a").some((t) => attr(t, "href") === releasesURL)).toBe(true);
    });
  }
});
