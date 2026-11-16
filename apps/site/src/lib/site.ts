// The site's constants and its languages, shared by the build (buildtime/) and the pages (src/). Nothing here touches the file system, so the pages can import it.

// siteURL is where the site is served, the address of canonical links, Open Graph tags and the sitemap. CNAME names it too, though the GitHub Pages site takes its custom domain from the repository's settings.
export const siteURL = "https://spruce.yldm.ai";

// themeColor is the deepest violet of the app's background (apps/spruce/internal/ui/kit/tokens.go, ColorBgDeep), the browser's color around the page and the web app manifest's theme and background.
export const themeColor = "#1c0f26";

export const repoURL = "https://github.com/yldm-tech/spruce";
export const releasesURL = repoURL + "/releases";
// latestURL redirects to the newest published release, for what has to stay current after a release comes out: the install commands and the structured data's download link. Drafts and prereleases are not "latest", so it never lands on a release that 404s for visitors.
export const latestURL = releasesURL + "/latest";
export const licenseURL = repoURL + "/blob/main/LICENSE";
export const issuesURL = repoURL + "/issues";
export const changelogURL = repoURL + "/blob/main/apps/spruce/CHANGELOG.md";

// The names nsgo build gives a release's packages (.github/workflows/package.yml), by version and architecture: arm64, or amd64 for x86-64, which nsgo build names x64.
const packageArch = (arch: string) => (arch === "amd64" ? "x64" : arch);
export const macDisk = (version: string, arch: string) => `spruce-${version}-macos-${packageArch(arch)}.dmg`;
export const windowsSetup = (version: string, arch: string) => `spruce-${version}-windows-${packageArch(arch)}-setup.exe`;

// releaseFileURL downloads the file name of release version. The site is deployed only once its version is published (.github/workflows/site.yml), so the Download buttons fetch the file straight away rather than opening the release's page.
export function releaseFileURL(version: string, name: string): string {
  return `${releasesURL}/download/v${version}/${name}`;
}

// checksumsFile is the list of the release's SHA-256 checksums that nsgo build writes beside the packages.
export const checksumsFile = "checksums.txt";

// The install scripts nsgo build adds to every release: install.sh picks the Mac's architecture, checks the disk image's SHA-256 and installs the app; install.ps1 does the same on Windows. They come from the newest release, so the commands never name a version.
export const macInstallCommand = "curl -fsSL " + latestURL + "/download/install.sh | sh";
export const windowsInstallCommand = "irm " + latestURL + "/download/install.ps1 | iex";

// Language is one page of the site, in the order the switcher lists them.
export interface Language {
  // code names the content file and the page's folder.
  code: LangCode;
  // tag is the BCP 47 tag of the page, for <html lang> and hreflang: a script rather than a region for Chinese, so that the Simplified page also serves Singapore and the Traditional one Hong Kong and Macau.
  tag: string;
  // locale is the Open Graph locale.
  locale: string;
  // name is the language named in itself, as the switcher shows it.
  name: string;
  // readme is the README file written in this language.
  readme: string;
  // cliSection is the anchor GitHub gives that README's Command line section, from its heading.
  cliSection: string;
}

export const langCodes = ["en", "zh-CN", "zh-TW", "ja", "ko"] as const;
export type LangCode = (typeof langCodes)[number];

// otherLangCodes are the languages whose pages live in a folder of their own, the values of the $lang route segment.
export const otherLangCodes = ["zh-CN", "zh-TW", "ja", "ko"] as const;
export type OtherLangCode = (typeof otherLangCodes)[number];

export const languages: readonly Language[] = [
  { code: "en", tag: "en", locale: "en_US", name: "English", readme: "README.md", cliSection: "command-line" },
  { code: "zh-CN", tag: "zh-Hans", locale: "zh_CN", name: "简体中文", readme: "README.zh-CN.md", cliSection: "命令行" },
  { code: "zh-TW", tag: "zh-Hant", locale: "zh_TW", name: "繁體中文", readme: "README.zh-TW.md", cliSection: "命令列" },
  { code: "ja", tag: "ja", locale: "ja_JP", name: "日本語", readme: "README.ja.md", cliSection: "コマンドライン" },
  { code: "ko", tag: "ko", locale: "ko_KR", name: "한국어", readme: "README.ko.md", cliSection: "명령줄" },
];

// sourceLang is the language the other content files are translated from, and the page at the root.
export const sourceLang: LangCode = "en";

// appLang is the language of the app's interface, which is in Simplified Chinese only: the screenshots show it, and the structured data names it as the app's only language.
export const appLang = "zh-Hans";

export function language(code: LangCode): Language {
  const l = languages.find((x) => x.code === code);
  if (!l) {
    throw new Error("no language " + code);
  }
  return l;
}

// pagePath is the path of a language's page: the root for English, a folder named after the language for the others.
export function pagePath(code: LangCode): string {
  return code === sourceLang ? "/" : "/" + code + "/";
}

// pageURL is a page's absolute address.
export function pageURL(code: LangCode): string {
  return siteURL + pagePath(code);
}

// readmeURL is the README in this language on GitHub.
export function readmeURL(l: Language): string {
  return repoURL + "/blob/main/" + l.readme;
}

// shotNames are the screenshots of the app the pages show, docs/assets/<name>.png: the screen tests' fixture images, the same in every language because the app's interface is Simplified Chinese only.
export const shotNames = [
  "overview-ready",
  "overview-toast-undo",
  "history-restore",
  "space-results",
  "space-growth-7d",
  "uninstall-detail",
  "settings-auto-on",
  "dev-projects",
  "health-idle",
] as const;
export type ShotName = (typeof shotNames)[number];

// faqPairs are the questions of the FAQ section in its order, each a question's content key and its answer's; the home pages' structured data (buildtime/jsonld.ts) lists the same.
export const faqPairs = [
  ["faqAskFirstQ", "faqAskFirstA"],
  ["faqRestoreQ", "faqRestoreA"],
  ["faqBackgroundQ", "faqBackgroundA"],
  ["faqDataQ", "faqDataA"],
  ["faqDiskAccessQ", "faqDiskAccessA"],
  ["faqPasswordQ", "faqPasswordA"],
  ["faqCodeQ", "faqCodeA"],
  ["faqWindowsQ", "faqWindowsA"],
  ["faqEnglishQ", "faqEnglishA"],
  ["faqLinuxQ", "faqLinuxA"],
  ["faqUpdateQ", "faqUpdateA"],
  ["faqPriceQ", "faqPriceA"],
] as const;
