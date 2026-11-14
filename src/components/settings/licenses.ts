/** One third-party library Spruce ships, for 关于 → 开源许可. */
export interface License {
  name: string;
  /** Its SPDX licence identifier. */
  license: string;
}

/** The main libraries in the app (go.mod and package.json), kept by hand with the dependencies: every one is under a permissive licence. */
export const LICENSES: readonly License[] = [
  { name: "MyGo", license: "MIT" },
  { name: "purego", license: "Apache-2.0" },
  { name: "gopsutil", license: "BSD-3-Clause" },
  { name: "Go x/sys", license: "BSD-3-Clause" },
  { name: "React", license: "MIT" },
  { name: "TanStack Query、Router、Virtual", license: "MIT" },
  { name: "filesize.js", license: "BSD-3-Clause" },
  { name: "Inter 字体", license: "OFL-1.1" },
];
