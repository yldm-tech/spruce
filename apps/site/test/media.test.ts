// The images scripts/media.ts commits under media/ are there for every language and screenshot and were made from what each shows now, so that a changed headline, template or screenshot cannot reach the site with an old picture.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vite-plus/test";
import { pngSize } from "../buildtime/icons";
import {
  inputsHash,
  mediaCommand,
  mediaDir,
  mediaLangs,
  mediaShots,
  ogFile,
  ogSize,
  readMediaManifest,
  shotFile,
  shotWidths,
  staleMedia,
} from "../buildtime/media";
import { shotNames } from "../src/lib/site";
import { builtLangs, repoRoot, siteDir } from "./helpers";

const langs = mediaLangs(repoRoot, siteDir, join(siteDir, "content"));
const shots = mediaShots(repoRoot);

describe("media", () => {
  it("has every image, made from the inputs it has now", () => {
    const stale = staleMedia(siteDir, langs, shots);
    expect(stale, `out of date: run ${mediaCommand} in apps/site to make these again`).toEqual([]);
  });

  it("records in its manifest the languages that have pages and the screenshots the pages show, and no others", () => {
    const manifest = readMediaManifest(siteDir);
    expect(Object.keys(manifest.og).sort()).toEqual(
      builtLangs()
        .map((l) => l.code)
        .sort(),
    );
    expect(Object.keys(manifest.shots).sort()).toEqual([...shotNames].sort());
  });

  it("makes the link preview images 1200×630", () => {
    for (const l of builtLangs()) {
      expect(pngSize(join(siteDir, mediaDir, ogFile(l.code))), l.code).toEqual(ogSize);
    }
  });

  it("finds an image stale when what it shows changes", () => {
    const m = langs[0];
    const s = shots[0];
    if (!m || !s) {
      throw new Error("no languages or screenshots");
    }
    const changed = { ...m, og: { ...m.og, heroTitle: m.og["heroTitle"] + " again" } };
    expect(inputsHash(changed.og)).not.toBe(inputsHash(m.og));
    expect(staleMedia(siteDir, [changed], [])).toEqual([join(mediaDir, ogFile(m.lang.code))]);
    const png = { ...s, formats: { ...s.formats, webp: { ...s.formats.webp, png: "0".repeat(64) } } };
    expect(staleMedia(siteDir, [], [png])).toEqual(shotWidths.map((w) => join(mediaDir, shotFile(s.name, w, "webp"))));
  });

  it("encodes the screenshots in their formats", () => {
    for (const name of shotNames) {
      for (const w of shotWidths) {
        const avif = readFileSync(join(siteDir, mediaDir, shotFile(name, w, "avif")));
        expect(avif.toString("latin1", 4, 12), `${name} ${w} avif`).toBe("ftypavif");
        const webp = readFileSync(join(siteDir, mediaDir, shotFile(name, w, "webp")));
        expect([webp.toString("latin1", 0, 4), webp.toString("latin1", 8, 12)], `${name} ${w} webp`).toEqual(["RIFF", "WEBP"]);
      }
    }
  });
});
