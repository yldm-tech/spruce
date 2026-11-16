// The modules the build makes from the repository (buildtime/plugin.ts).

declare module "virtual:spruce/site" {
  const site: import("../buildtime/schema").SiteData;
  export default site;
}

declare module "virtual:spruce/content/*" {
  const content: import("../buildtime/schema").ContentModule;
  export default content;
}

declare module "virtual:spruce/content-loaders" {
  const loaders: Partial<Record<import("./lib/site").LangCode, () => Promise<{ default: import("../buildtime/schema").ContentModule }>>>;
  export default loaders;
}
