// The download section's install commands are coloured as the design's terminals are, and putting the tokens back together gives the command unchanged.
import { expect, it } from "vite-plus/test";
import { highlight } from "../src/lib/highlight";
import { macInstallCommand, windowsInstallCommand } from "../src/lib/site";
import { builtLangs, pageFile, readOut } from "./helpers";

it("colours the command, subcommand, flags, links and operators", () => {
  const kinds = (line: string) =>
    highlight(line)
      .filter((t) => t.kind !== "")
      .map((t) => `${t.kind}:${t.text}`);
  expect(kinds(macInstallCommand)).toEqual(["k:curl", "f:-fsSL", "u:https://github.com/yldm-tech/spruce/releases/latest/download/install.sh", "o:|", "k:sh"]);
  expect(kinds(windowsInstallCommand)).toEqual(["k:irm", "u:https://github.com/yldm-tech/spruce/releases/latest/download/install.ps1", "o:|", "k:iex"]);
  expect(kinds("spruce clean --dry-run")).toEqual(["k:spruce", "s:clean", "f:--dry-run"]);
  expect(kinds("# a comment")).toEqual(["c:# a comment"]);
  for (const line of [macInstallCommand, windowsInstallCommand, "  cd  spruce ", "# note", ""]) {
    expect(
      highlight(line)
        .map((t) => t.text)
        .join(""),
    ).toBe(line);
  }
});

it("shows the install commands coloured, each after its shell's prompt", () => {
  for (const l of builtLangs()) {
    const html = readOut(pageFile(l));
    const blocks = [...html.matchAll(/<div class="code"><pre><code>([\s\S]*?)<\/code><\/pre>/g)].map((m) => m[1] ?? "");
    expect(blocks.length, l.code).toBe(2);
    for (const block of blocks) {
      expect(block, l.code).toMatch(/<span class="k">/);
    }
    expect(blocks[0], l.code).toMatch(/^<span class="p" aria-hidden="true">\$ <\/span>/);
    expect(blocks[1], l.code).toMatch(/^<span class="p" aria-hidden="true">PS&gt; <\/span>/);
  }
});
