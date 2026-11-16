// The images the dev script scripts/media.ts makes and commits under media/, which the build copies into the site: a link preview image for each language (og/<code>.png: the app icon, the headline and a short line beside a crop of the overview screenshot), and each screenshot of the app as AVIF and WebP in two widths (shots/<name>-<width>.<format>), which the pages offer before the PNG. Making them needs a browser, ffmpeg and cwebp, so they are not made by vp build; instead media/manifest.json records, for each image, a hash of the inputs it was made from, and the tests fail when an image's inputs no longer hash to what the manifest says (staleMedia), naming the command that makes it again.
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { z } from "zod";
import { languages, shotNames, type Language, type LangCode, type ShotName } from "../src/lib/site";
import { loadContent, readJSON, type Strings } from "./content";
import { langCodeSchema, shotNameSchema } from "./schema";

// mediaDir is the folder of the committed images, relative to the site's folder.
export const mediaDir = "media";

// mediaCommand makes the images again, run in apps/site.
export const mediaCommand = "vp run media";

// ogTemplate is the HTML page the link preview image is a screenshot of, relative to the site's folder; the script fills in its {{name}} fields.
export const ogTemplate = "scripts/og.html";

// ogSize is the link preview image's size, the 1.91:1 that Open Graph and X's large summary card show uncropped.
export const ogSize = { w: 1200, h: 630 } as const;

// iconFile is the app icon, relative to the repository root; the icons of the site and the link preview image are made from it.
export const iconFile = "apps/spruce/assets/Icon.png";

// shotDir is the folder of the screenshots, relative to the repository root: docs/assets/<name>.png.
export const shotDir = "docs/assets";

// ogShot is the screenshot the link preview image shows beside the headline.
export const ogShot: ShotName = "overview-ready";

// ogKeys are the content strings the link preview image shows: the headline with its mark, and the short line under it.
export const ogKeys = ["heroTitle", "heroTitleMark", "ogTagline"] as const;

// shotWidths are the widths each screenshot is offered in: half of the PNG's for small screens and one-times displays, and the PNG's own.
export const shotWidths = [640, 1280] as const;
export const shotFormats = ["avif", "webp"] as const;
export type ShotFormat = (typeof shotFormats)[number];

// shotEncoder is the encoder's command for an image width wide: ffmpeg's SVT-AV1 for AVIF, and libwebp's cwebp for WebP, as Homebrew's ffmpeg has no WebP encoder. The arguments are part of the shots' inputs, so a change to them makes the images again.
export function shotEncoder(format: ShotFormat, src: string, width: number, out: string): { cmd: string; args: string[] } {
  if (format === "avif") {
    return {
      cmd: "ffmpeg",
      args: [
        "-hide_banner",
        "-loglevel",
        "error",
        "-y",
        "-i",
        src,
        "-vf",
        `scale=${width}:-1:flags=lanczos`,
        "-c:v",
        "libsvtav1",
        "-preset",
        "4",
        "-crf",
        "30",
        "-pix_fmt",
        "yuv420p",
        "-frames:v",
        "1",
        "-f",
        "avif",
        out,
      ],
    };
  }
  return { cmd: "cwebp", args: ["-quiet", "-q", "82", "-m", "6", "-resize", String(width), "0", src, "-o", out] };
}

// shotPNG is the screenshot's PNG, relative to the repository root.
export function shotPNG(name: ShotName): string {
  return `${shotDir}/${name}.png`;
}

// shotFile is the path of a screenshot in a width and format, relative to mediaDir and to the built site's root.
export function shotFile(name: ShotName, width: number, format: ShotFormat): string {
  return `shots/${name}-${width}.${format}`;
}

export function ogFile(code: LangCode): string {
  return `og/${code}.png`;
}

export function sha256(data: string | Uint8Array): string {
  return createHash("sha256").update(data).digest("hex");
}

// hashLink is path with the start of the hash of data, its file's content, in the query, so that a browser fetches a changed file again rather than keeping the copy it has.
export function hashLink(path: string, data: string | Uint8Array): string {
  return path + "?v=" + sha256(data).slice(0, 10);
}

// Inputs are what an image is made from, by name: the text it shows and the hashes of the files it is drawn from.
export type Inputs = Record<string, string>;

// inputsHash is the hash of inputs that the manifest records, independent of their order.
export function inputsHash(inputs: Inputs): string {
  return sha256(JSON.stringify(Object.entries(inputs).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))));
}

// ogInputs are the inputs of a language's link preview image: its strings, the template, the app icon and the screenshot.
export function ogInputs(root: string, siteDir: string, strings: Strings): Inputs {
  const inputs: Inputs = {
    template: sha256(readFileSync(join(siteDir, ogTemplate))),
    icon: sha256(readFileSync(join(root, iconFile))),
    shot: sha256(readFileSync(join(root, shotPNG(ogShot)))),
  };
  for (const key of ogKeys) {
    const s = strings[key];
    if (s === undefined) {
      throw new Error(`no content key ${key}`);
    }
    inputs[key] = s;
  }
  return inputs;
}

// shotInputs are the inputs of a screenshot in a format: the PNG and the encoder's arguments.
export function shotInputs(root: string, name: ShotName, format: ShotFormat): Inputs {
  const inputs: Inputs = { png: sha256(readFileSync(join(root, shotPNG(name)))) };
  for (const width of shotWidths) {
    const enc = shotEncoder(format, "in.png", width, "out");
    inputs["encoder" + width] = [enc.cmd, ...enc.args].join(" ");
  }
  return inputs;
}

const hashSchema = z.string().regex(/^[0-9a-f]{64}$/, "is not a sha256");

// mediaManifestSchema is media/manifest.json: the inputs hash each image was made from, the link preview images by language and the screenshots by name and format.
export const mediaManifestSchema = z.strictObject({
  og: z.partialRecord(langCodeSchema, hashSchema),
  shots: z.partialRecord(shotNameSchema, z.partialRecord(z.enum(shotFormats), hashSchema)),
});
export type MediaManifest = z.infer<typeof mediaManifestSchema>;

export function readMediaManifest(siteDir: string): MediaManifest {
  const path = join(siteDir, mediaDir, "manifest.json");
  if (!existsSync(path)) {
    return { og: {}, shots: {} };
  }
  return mediaManifestSchema.parse(readJSON(path));
}

// MediaLang is what a language's link preview image is made from.
export interface MediaLang {
  lang: Language;
  strings: Strings;
  og: Inputs;
}

// MediaShot is what a screenshot's smaller formats are made from.
export interface MediaShot {
  name: ShotName;
  formats: Record<ShotFormat, Inputs>;
}

// mediaLangs reads the inputs of every language with a content file, as the build reads them.
export function mediaLangs(root: string, siteDir: string, contentDir: string): MediaLang[] {
  const content = loadContent(contentDir);
  return languages
    .filter((l) => content.has(l.code))
    .map((l) => {
      const strings = content.get(l.code) ?? {};
      return { lang: l, strings, og: ogInputs(root, siteDir, strings) };
    });
}

// mediaShots reads the inputs of every screenshot.
export function mediaShots(root: string): MediaShot[] {
  return shotNames.map((name) => ({ name, formats: { avif: shotInputs(root, name, "avif"), webp: shotInputs(root, name, "webp") } }));
}

// staleMedia lists the images of langs and shots that are missing or were made from other inputs than they have now.
export function staleMedia(siteDir: string, langs: readonly MediaLang[], shots: readonly MediaShot[]): string[] {
  const manifest = readMediaManifest(siteDir);
  const stale: string[] = [];
  for (const m of langs) {
    const code = m.lang.code;
    if (!existsSync(join(siteDir, mediaDir, ogFile(code))) || manifest.og[code] !== inputsHash(m.og)) {
      stale.push(join(mediaDir, ogFile(code)));
    }
  }
  for (const s of shots) {
    for (const format of shotFormats) {
      const files = shotWidths.map((w) => join(mediaDir, shotFile(s.name, w, format)));
      if (files.some((f) => !existsSync(join(siteDir, f))) || manifest.shots[s.name]?.[format] !== inputsHash(s.formats[format])) {
        stale.push(...files);
      }
    }
  }
  return stale;
}
