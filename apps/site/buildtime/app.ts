// What the page says about the app that it reads from the app's own files, so that the page follows them: the version. The reader fails the build when its source no longer has the shape it reads.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "yaml";
import { issues } from "./content";
import { manifestSchema } from "./schema";

// manifestFile is the app's nsgo.yaml, relative to the repository root.
export const manifestFile = "apps/spruce/nsgo.yaml";

function readYAML(path: string): unknown {
  try {
    return parse(readFileSync(path, "utf8"));
  } catch (err) {
    throw new Error(`${path}: ${(err as Error).message}`, { cause: err });
  }
}

// appVersion is the version in apps/spruce/nsgo.yaml, which the Release workflow tags and publishes.
export function appVersion(root: string): string {
  const res = manifestSchema.safeParse(readYAML(join(root, manifestFile)));
  if (!res.success) {
    throw issues(manifestFile, res.error);
  }
  return res.data.version;
}
