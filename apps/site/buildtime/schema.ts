// The shapes of what the build reads from the repository and of the data it hands the pages. Each file read is parsed with one of these zod schemas, so that a source that changes shape fails the build instead of rendering a page with a hole.
import { z } from "zod";
import { langCodes, shotNames } from "../src/lib/site";

// ---- what the build reads ----

// placeholderRe finds a value a content string names, such as {version}, filled in when the page is rendered.
export const placeholderRe = /\{[a-zA-Z]+\}/g;

export function placeholders(s: string): string[] {
  return [...(s.match(placeholderRe) ?? [])].sort();
}

const nonEmpty = z.string().refine((s) => s.trim() !== "", "is empty");

// contentFileSchema is a content file before it is compared with en.json: a flat object of strings, none empty.
export const contentFileSchema = z.record(z.string(), nonEmpty);

// contentSchema is a content file of the language code checked against en.json, the source: exactly its keys, the same placeholders in each string, ` in pairs, and heroTitleMark inside heroTitle.
export function contentSchema(source: Record<string, string>) {
  const shape: Record<string, z.ZodType<string>> = {};
  for (const key of Object.keys(source)) {
    shape[key] = nonEmpty;
  }
  return z.strictObject(shape).superRefine((strs, ctx) => {
    for (const [key, s] of Object.entries(strs)) {
      const want = placeholders(source[key] ?? "");
      const got = placeholders(s);
      if (want.join() !== got.join()) {
        ctx.addIssue({ code: "custom", path: [key], message: `has placeholders [${got.join(" ")}], en.json has [${want.join(" ")}]` });
      }
      if ((s.split("`").length - 1) % 2 !== 0) {
        ctx.addIssue({ code: "custom", path: [key], message: "has a ` that opens code it never closes" });
      }
    }
    const title = strs["heroTitle"];
    const mark = strs["heroTitleMark"];
    if (title !== undefined && mark !== undefined && !title.includes(mark)) {
      ctx.addIssue({ code: "custom", path: ["heroTitleMark"], message: `${JSON.stringify(mark)} is not in heroTitle ${JSON.stringify(title)}` });
    }
  });
}

// manifestSchema is the part of apps/spruce/nsgo.yaml the site reads: the version the Release workflow tags and publishes.
export const manifestSchema = z.object({ version: z.string().regex(/^\d+\.\d+\.\d+$/, "is not a version") });

// ---- what the build hands the pages ----

export const langCodeSchema = z.enum(langCodes);
export const shotNameSchema = z.enum(shotNames);

export const shotSchema = z.object({ src: z.string(), w: z.number().int().positive(), h: z.number().int().positive() });
export type Shot = z.infer<typeof shotSchema>;

// pictureSchema is a screenshot offered in smaller formats before its PNG: a <source> of each, by MIME type, with the widths it comes in.
export const pictureSchema = shotSchema.extend({ sources: z.array(z.object({ type: z.string(), srcset: z.string() })).min(1) });
export type Picture = z.infer<typeof pictureSchema>;

// missingSchema is 404.html's text in one language.
export const missingSchema = z.object({ code: langCodeSchema, tag: z.string(), title: z.string(), body: z.string(), home: z.string() });

// siteDataSchema is virtual:spruce/site, what every page reads.
export const siteDataSchema = z.object({
  // version is the app's version in apps/spruce/nsgo.yaml, whose release files the Download buttons fetch.
  version: z.string(),
  // shots are the screenshots of the app (docs/assets/<name>.png), each offered as AVIF and WebP before its PNG.
  shots: z.record(shotNameSchema, pictureSchema),
  // og is each language's link preview image (media/og/<code>.png).
  og: z.partialRecord(langCodeSchema, shotSchema),
  // tray is the menu bar icon and its low-space form (apps/spruce/assets/tray.png and tray-alert.png).
  tray: z.object({ normal: shotSchema, alert: shotSchema }),
  // assets are the static scripts, linked with a hash of their content in the query so that a changed file is fetched again.
  assets: z.object({ siteJS: z.string() }),
  missing: z.array(missingSchema),
});
export type SiteData = z.infer<typeof siteDataSchema>;

// contentModuleSchema is virtual:spruce/content/<code>: a language's content strings.
export const contentModuleSchema = z.object({
  code: langCodeSchema,
  strings: z.record(z.string(), z.string()),
});
export type ContentModule = z.infer<typeof contentModuleSchema>;
