// The structured data of each page, as JSON-LD in a <script type="application/ld+json"> data block, which the browser never runs, so the Content Security Policy stays as strict as it is (buildtime/csp.ts leaves data blocks out of the hashes). Every page describes the app (SoftwareApplication), its publisher (Organization), the site in its language (WebSite) and its questions (FAQPage). Each graph is checked with the zod schemas below, so that a missing or misshapen field fails the build rather than reaching the search engines. The data is put into the prerendered HTML after the build (finishHTML), so that it adds nothing to the scripts the pages load.
import { z } from "zod";
import { appLang, faqPairs, languages, latestURL, licenseURL, pageURL, repoURL, siteURL, type LangCode } from "../src/lib/site";
import { fill, type Strings } from "./content";
import type { Shot } from "./schema";

export const schemaContext = "https://schema.org";

// The entities every page names, by their @id: one app and one publisher, whatever the page's language.
export const appID = siteURL + "/#app";
export const orgID = siteURL + "/#organization";

const text = z.string().refine((s) => s.trim() !== "" && !s.includes("`") && !/\{[a-zA-Z]+\}/.test(s), "is empty or has markup or a placeholder left in it");
const url = z.url({ protocol: /^https$/ });
const ref = z.strictObject({ "@id": url });

export const organizationSchema = z.strictObject({
  "@type": z.literal("Organization"),
  "@id": z.literal(orgID),
  name: z.literal("YLDM"),
  sameAs: z.array(url).min(1),
});

export const imageObjectSchema = z.strictObject({
  "@type": z.literal("ImageObject"),
  url,
  width: z.number().int().positive(),
  height: z.number().int().positive(),
  caption: text,
});

export const softwareApplicationSchema = z.strictObject({
  "@type": z.literal("SoftwareApplication"),
  "@id": z.literal(appID),
  name: z.literal("Spruce"),
  description: text,
  url,
  image: url,
  operatingSystem: z.literal("macOS, Windows"),
  applicationCategory: z.literal("UtilitiesApplication"),
  offers: z.strictObject({ "@type": z.literal("Offer"), price: z.literal("0"), priceCurrency: z.literal("USD") }),
  isAccessibleForFree: z.literal(true),
  softwareVersion: z.string().regex(/^\d+\.\d+\.\d+$/),
  downloadUrl: z.literal(latestURL),
  license: z.literal(licenseURL),
  screenshot: imageObjectSchema,
  // inLanguage is the app's interface language, Simplified Chinese only, whatever the page's language.
  inLanguage: z.array(z.literal(appLang)).length(1),
  author: ref,
  publisher: ref,
});

export const webSiteSchema = z.strictObject({
  "@type": z.literal("WebSite"),
  "@id": url,
  url,
  name: z.literal("Spruce"),
  description: text,
  inLanguage: z.string().min(2),
  publisher: ref,
  about: ref,
});

export const faqPageSchema = z.strictObject({
  "@type": z.literal("FAQPage"),
  "@id": url,
  url,
  inLanguage: z.string().min(2),
  mainEntity: z
    .array(
      z.strictObject({
        "@type": z.literal("Question"),
        name: text,
        acceptedAnswer: z.strictObject({ "@type": z.literal("Answer"), text }),
      }),
    )
    .min(1),
});

export const graphSchema = z.strictObject({
  "@context": z.literal(schemaContext),
  "@graph": z.array(z.discriminatedUnion("@type", [organizationSchema, softwareApplicationSchema, webSiteSchema, faqPageSchema])).min(4),
});
export type Graph = z.infer<typeof graphSchema>;

// plainLine is s with its line breaks taken out: nothing after a Chinese character, kana or CJK punctuation mark, as Chinese and Japanese put no space between words, and a space after anything else, Korean among it.
export function plainLine(s: string): string {
  let out = "";
  let prev = "";
  for (const ch of s) {
    if (ch === "\n") {
      const cp = prev.codePointAt(0) ?? 0;
      const unspaced = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}]/u.test(prev) || (cp >= 0x3000 && cp <= 0x303f) || (cp >= 0xff00 && cp <= 0xffef);
      if (!unspaced) {
        out += " ";
      }
      continue;
    }
    out += ch;
    prev = ch;
  }
  return out;
}

export interface PageInput {
  code: LangCode;
  strings: Strings;
  version: string;
  // shot is the screenshot the app's entry shows, the overview.
  shot: Shot;
}

// structuredData is the graph of a language's page, checked against graphSchema.
export function structuredData(p: PageInput): Graph {
  const lang = languages.find((l) => l.code === p.code);
  if (!lang) {
    throw new Error("no language " + p.code);
  }
  const t = (key: string): string => {
    const s = p.strings[key];
    if (s === undefined) {
      throw new Error(`no content key ${key} in ${p.code}`);
    }
    // The page sets the names between ` in code and breaks some headings in two; structured data is plain text.
    return plainLine(fill(key, s, { version: p.version }).replaceAll("`", ""));
  };
  const home = pageURL(p.code);
  const graph: unknown[] = [
    { "@type": "Organization", "@id": orgID, name: "YLDM", sameAs: [repoURL] },
    {
      "@type": "SoftwareApplication",
      "@id": appID,
      name: "Spruce",
      description: t("metaDescription"),
      url: home,
      image: siteURL + "/icon-512.png",
      operatingSystem: "macOS, Windows",
      applicationCategory: "UtilitiesApplication",
      offers: { "@type": "Offer", price: "0", priceCurrency: "USD" },
      isAccessibleForFree: true,
      softwareVersion: p.version,
      downloadUrl: latestURL,
      license: licenseURL,
      screenshot: { "@type": "ImageObject", url: siteURL + p.shot.src, width: p.shot.w, height: p.shot.h, caption: t("shotOverviewAlt") },
      inLanguage: [appLang],
      author: { "@id": orgID },
      publisher: { "@id": orgID },
    },
    {
      "@type": "WebSite",
      "@id": home + "#website",
      url: home,
      name: "Spruce",
      description: t("metaDescription"),
      inLanguage: lang.tag,
      publisher: { "@id": orgID },
      about: { "@id": appID },
    },
    {
      "@type": "FAQPage",
      "@id": home + "#faq",
      url: home,
      inLanguage: lang.tag,
      mainEntity: faqPairs.map(([q, a]) => ({ "@type": "Question", name: t(q), acceptedAnswer: { "@type": "Answer", text: t(a) } })),
    },
  ];
  const res = graphSchema.safeParse({ "@context": schemaContext, "@graph": graph });
  if (!res.success) {
    throw new Error(res.error.issues.map((i) => `structured data of ${home}: ${i.path.join(".")} ${i.message}`).join("\n"));
  }
  return res.data;
}
