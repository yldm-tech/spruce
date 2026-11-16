// Makes the committed images under media/ (buildtime/media.ts): each language's link preview image and each screenshot as AVIF and WebP. Run it in apps/site with `vp run media` after changing what an image shows; the tests name the images that are out of date. It makes only those, or every image with --all.
//
// The link preview image is a screenshot of scripts/og.html, filled in with the language's headline and short line beside the overview screenshot, taken in a headless Chromium (SPRUCE_CHROME, by default Playwright's chrome-headless-shell) driven over the DevTools protocol with Node's own WebSocket, in a profile of its own that is removed afterwards with the browser. The smaller formats take ffmpeg (with SVT-AV1) and cwebp, from Homebrew's ffmpeg and webp.
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { PNG } from "pngjs";
import {
  iconFile,
  inputsHash,
  mediaDir,
  mediaLangs,
  mediaShots,
  ogFile,
  ogShot,
  ogSize,
  ogTemplate,
  readMediaManifest,
  shotEncoder,
  shotFile,
  shotFormats,
  shotPNG,
  shotWidths,
  staleMedia,
  type MediaLang,
  type MediaManifest,
  type MediaShot,
} from "../buildtime/media";
import { encodePNG, logoSize, opaqueSquare, premultiply, shrink } from "../buildtime/icons";

const siteDir = process.cwd();
const root = resolve(siteDir, "../..");
const contentDir = join(siteDir, "content");
const chromePath =
  process.env["SPRUCE_CHROME"] ??
  join(homedir(), "Library/Caches/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-mac-arm64/chrome-headless-shell");

function need(cmd: string, hint: string): void {
  if (spawnSync("sh", ["-c", `command -v ${cmd}`]).status !== 0) {
    throw new Error(`${cmd} is not installed; ${hint}`);
  }
}

// run runs a command, and prints what it printed only when it fails, as SVT-AV1 logs its settings whatever ffmpeg's log level.
function run(cmd: string, args: string[]): void {
  const res = spawnSync(cmd, args, { stdio: ["ignore", "pipe", "pipe"], encoding: "utf8", timeout: 120_000 });
  if (res.status !== 0) {
    throw new Error(`${cmd} ${args.join(" ")} failed (${res.status ?? res.signal}):\n${res.stdout}${res.stderr}`);
  }
}

// ---- the screenshots ----

function makeShots(s: MediaShot, manifest: MediaManifest): void {
  const src = join(root, shotPNG(s.name));
  for (const format of shotFormats) {
    for (const width of shotWidths) {
      const out = join(siteDir, mediaDir, shotFile(s.name, width, format));
      mkdirSync(dirname(out), { recursive: true });
      const enc = shotEncoder(format, src, width, out);
      run(enc.cmd, enc.args);
      console.log("made", join(mediaDir, shotFile(s.name, width, format)));
    }
    (manifest.shots[s.name] ??= {})[format] = inputsHash(s.formats[format]);
  }
}

// ---- the browser ----

// CDP is a connection to a page target of the browser over the DevTools protocol.
class CDP {
  private next = 0;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private waiters = new Map<string, (() => void)[]>();

  private constructor(private ws: WebSocket) {
    ws.addEventListener("message", (event) => {
      const msg = JSON.parse(String(event.data)) as { id?: number; method?: string; result?: unknown; error?: { message: string } };
      if (msg.id !== undefined) {
        const p = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        if (msg.error) {
          p?.reject(new Error(msg.error.message));
        } else {
          p?.resolve(msg.result);
        }
      } else if (msg.method) {
        const list = this.waiters.get(msg.method) ?? [];
        this.waiters.delete(msg.method);
        for (const w of list) {
          w();
        }
      }
    });
  }

  static open(url: string): Promise<CDP> {
    return new Promise((ok, fail) => {
      const ws = new WebSocket(url);
      ws.addEventListener("open", () => ok(new CDP(ws)), { once: true });
      ws.addEventListener("error", () => fail(new Error("cannot connect to " + url)), { once: true });
    });
  }

  send<T = Record<string, unknown>>(method: string, params: Record<string, unknown> = {}): Promise<T> {
    const id = ++this.next;
    this.ws.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
    });
  }

  event(method: string): Promise<void> {
    return new Promise((resolve) => {
      this.waiters.set(method, [...(this.waiters.get(method) ?? []), resolve]);
    });
  }

  close(): void {
    this.ws.close();
  }
}

interface Browser {
  page: CDP;
  close: () => Promise<void>;
}

// abort kills the browser and removes its profile when the script is interrupted, as the finally blocks do not run then.
let abort: (() => void) | undefined;
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => {
    abort?.();
    process.exit(130);
  });
}

async function waitFor<T>(what: string, get: () => T | undefined, ms = 20_000): Promise<T> {
  const end = Date.now() + ms;
  for (;;) {
    const v = get();
    if (v !== undefined) {
      return v;
    }
    if (Date.now() > end) {
      throw new Error("timed out waiting for " + what);
    }
    await new Promise((r) => setTimeout(r, 100));
  }
}

// launch starts the browser in a new profile and opens a page. close quits it, kills it if it does not quit, and removes the profile. The browser leads a process group of its own, which is signalled whole: its helper processes, such as the network service, outlive a browser process killed alone, reparented to launchd.
async function launch(): Promise<Browser> {
  if (!existsSync(chromePath)) {
    throw new Error(`no browser at ${chromePath}; install it with npx playwright install chromium-headless-shell, or point SPRUCE_CHROME at a Chromium`);
  }
  const profile = mkdtempSync(join(tmpdir(), "spruce-media-chrome-"));
  const chrome: ChildProcess = spawn(
    chromePath,
    [
      `--user-data-dir=${profile}`,
      "--remote-debugging-port=0",
      "--headless",
      "--hide-scrollbars",
      "--no-first-run",
      "--no-default-browser-check",
      "--force-color-profile=srgb",
      "--lang=en-US",
      "about:blank",
    ],
    { stdio: "ignore", detached: true },
  );
  let exited = false;
  chrome.on("exit", () => {
    exited = true;
  });
  // signal sends sig to the browser's process group; a group that is gone already is no error.
  const signal = (sig: NodeJS.Signals) => {
    if (chrome.pid === undefined) {
      return;
    }
    try {
      process.kill(-chrome.pid, sig);
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code !== "ESRCH") {
        throw err;
      }
    }
  };
  abort = () => {
    signal("SIGKILL");
    rmSync(profile, { recursive: true, force: true });
  };
  const close = async () => {
    abort = undefined;
    if (!exited) {
      signal("SIGTERM");
      await waitFor("the browser to quit", () => (exited ? true : undefined), 5_000).catch(() => undefined);
    }
    // Whatever of the group is left after the browser quit.
    signal("SIGKILL");
    rmSync(profile, { recursive: true, force: true });
  };
  try {
    const portFile = join(profile, "DevToolsActivePort");
    const port = await waitFor("the browser's DevTools port", () => (existsSync(portFile) ? readFileSync(portFile, "utf8").split("\n")[0] : undefined));
    const res = await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, { method: "PUT" });
    const target = (await res.json()) as { webSocketDebuggerUrl: string };
    const page = await CDP.open(target.webSocketDebuggerUrl);
    await page.send("Page.enable");
    return {
      page,
      close: async () => {
        page.close();
        await close();
      },
    };
  } catch (err) {
    await close();
    throw err;
  }
}

async function load(page: CDP, url: string, width: number, height: number, scale: number): Promise<void> {
  await page.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: scale, mobile: false });
  await page.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-reduced-motion", value: "reduce" }] });
  const loaded = page.event("Page.loadEventFired");
  await page.send("Page.navigate", { url });
  await loaded;
  // The fonts, and a moment for hydration and the layout that follows it.
  await page.send("Runtime.evaluate", { expression: "document.fonts.ready.then(() => new Promise((r) => setTimeout(r, 1000)))", awaitPromise: true });
}

async function screenshot(page: CDP, clip?: { x: number; y: number; width: number; height: number }): Promise<Buffer> {
  const params: Record<string, unknown> = { format: "png" };
  if (clip) {
    params["clip"] = { ...clip, scale: 1 };
    params["captureBeyondViewport"] = true;
  }
  const { data } = await page.send<{ data: string }>("Page.captureScreenshot", params);
  return Buffer.from(data, "base64");
}

// ---- the card, served ----

// serve serves the pages and images of files by path on a free port of the loopback.
function serve(files: Map<string, { type: string; body: string | Buffer }>): Promise<{ server: Server; base: string }> {
  const server = createServer((req, res) => {
    const path = decodeURIComponent(new URL(req.url ?? "/", "http://localhost").pathname);
    const file = files.get(path);
    if (!file) {
      res.writeHead(404);
      res.end();
      return;
    }
    res.writeHead(200, { "content-type": file.type });
    res.end(file.body);
  });
  return new Promise((ok) => {
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      ok({ server, base: `http://127.0.0.1:${port}` });
    });
  });
}

// ---- the link preview images ----

function escapeHTML(s: string): string {
  return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

// titleHTML is the headline as the hero sets it: the mark set apart, and each line break a <br>.
function titleHTML(title: string, mark: string): string {
  const at = title.indexOf(mark);
  const lines = (s: string) => escapeHTML(s).replaceAll("\n", "<br>");
  if (at < 0) {
    return lines(title);
  }
  return lines(title.slice(0, at)) + `<em class="mark">${lines(mark)}</em>` + lines(title.slice(at + mark.length));
}

function card(m: MediaLang): string {
  const fields: Record<string, string> = {
    lang: m.lang.tag,
    icon: "/__og/icon.png",
    shot: "/__og/shot.png",
    title: titleHTML(m.strings["heroTitle"] ?? "", m.strings["heroTitleMark"] ?? ""),
    tagline: escapeHTML(m.strings["ogTagline"] ?? ""),
  };
  return readFileSync(join(siteDir, ogTemplate), "utf8").replace(/\{\{(\w+)\}\}/g, (whole, name: string) => fields[name] ?? whole);
}

// logo is the app icon as the site's logo.png: its rounded square without the transparent margin, larger than the card draws it.
function logo(): Buffer {
  return encodePNG(shrink(opaqueSquare(premultiply(PNG.sync.read(readFileSync(join(root, iconFile))))), logoSize));
}

async function makeOG(langs: readonly MediaLang[], manifest: MediaManifest): Promise<void> {
  let browser: Browser | undefined;
  let server: Server | undefined;
  try {
    const files = new Map<string, { type: string; body: string | Buffer }>([
      ["/__og/icon.png", { type: "image/png", body: logo() }],
      ["/__og/shot.png", { type: "image/png", body: readFileSync(join(root, shotPNG(ogShot))) }],
    ]);
    for (const m of langs) {
      files.set(`/__og/${m.lang.code}.html`, { type: "text/html; charset=utf-8", body: card(m) });
    }
    const served = await serve(files);
    server = served.server;
    browser = await launch();
    for (const m of langs) {
      const code = m.lang.code;
      await load(browser.page, `${served.base}/__og/${code}.html`, ogSize.w, ogSize.h, 1);
      const png = await screenshot(browser.page);
      const file = join(siteDir, mediaDir, ogFile(code));
      mkdirSync(dirname(file), { recursive: true });
      writeFileSync(file, png);
      manifest.og[code] = inputsHash(m.og);
      console.log("made", join(mediaDir, ogFile(code)));
    }
  } finally {
    await browser?.close();
    server?.close();
  }
}

async function main(): Promise<void> {
  const all = process.argv.includes("--all");
  const langs = mediaLangs(root, siteDir, contentDir);
  const shots = mediaShots(root);
  const stale = new Set(staleMedia(siteDir, langs, shots));
  const ogLangs = langs.filter((m) => all || stale.has(join(mediaDir, ogFile(m.lang.code))));
  const staleShots = shots.filter((s) => all || shotFormats.some((f) => shotWidths.some((w) => stale.has(join(mediaDir, shotFile(s.name, w, f))))));
  if (ogLangs.length === 0 && staleShots.length === 0) {
    console.log("every image is up to date; --all makes them again anyway");
    return;
  }
  const manifest = readMediaManifest(siteDir);
  if (staleShots.length > 0) {
    need("ffmpeg", "brew install ffmpeg");
    need("cwebp", "brew install webp");
    for (const s of staleShots) {
      makeShots(s, manifest);
    }
  }
  if (ogLangs.length > 0) {
    await makeOG(ogLangs, manifest);
  }
  // Languages without a content file any more, and screenshots the pages no longer show, lose their entries.
  const codes = new Set<string>(langs.map((m) => m.lang.code));
  for (const code of Object.keys(manifest.og)) {
    if (!codes.has(code)) {
      delete manifest.og[code as keyof typeof manifest.og];
    }
  }
  const names = new Set<string>(shots.map((s) => s.name));
  for (const name of Object.keys(manifest.shots)) {
    if (!names.has(name)) {
      delete manifest.shots[name as keyof typeof manifest.shots];
    }
  }
  mkdirSync(join(siteDir, mediaDir), { recursive: true });
  writeFileSync(join(siteDir, mediaDir, "manifest.json"), JSON.stringify(manifest, null, 2) + "\n");
}

let failed = false;
await main().catch((err: unknown) => {
  console.error(err instanceof Error ? err.message : err);
  failed = true;
});
process.exitCode = failed ? 1 : 0;
