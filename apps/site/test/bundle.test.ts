// React preloads every image a page renders without loading="lazy". Only the logo, the header's mark and the hero's icon, is on the first screen, and on a phone it is the largest paint; anything else preloaded competes with it for the first round trips, so the screenshots are lazy and the logo is fetched first.
import { expect, it } from "vite-plus/test";
import { attr, htmlFiles, readOut, tags } from "./helpers";

it("preloads no image but the logo, which it fetches first", () => {
  for (const file of htmlFiles()) {
    const preloads = tags(readOut(file), "link").filter((t) => attr(t, "rel") === "preload" && attr(t, "as") === "image");
    expect(
      preloads.map((t) => attr(t, "href")),
      file,
    ).toEqual(["/logo.png"]);
    expect(
      preloads.map((t) => attr(t, "fetchpriority")),
      file,
    ).toEqual(["high"]);
  }
});
