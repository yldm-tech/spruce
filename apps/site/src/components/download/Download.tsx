// Download: a glass panel with the release and its links beside the downloads for each system, a tab each, every button fetching a file of the version in apps/spruce/nsgo.yaml.
import site from "virtual:spruce/site";
import { useWords } from "../../i18n/content";
import { checksumsFile, macDisk, macInstallCommand, releaseFileURL, releasesURL, repoURL, windowsInstallCommand, windowsSetup } from "../../lib/site";
import { CodeBlock } from "../common/CodeBlock";
import { AppleMark, GitHubMark, WindowsMark } from "../common/marks";
import { SlidingPill } from "../common/SlidingPill";
import { useTabs } from "../common/tabs";

export function Download() {
  const { t, tf, lang } = useWords();
  // Each tab's buttons download its system's packages of this release, one per architecture.
  const file = (name: (version: string, arch: string) => string, arch: string) => releaseFileURL(site.version, name(site.version, arch));
  const tabs = useTabs([
    ["tab-mac", "panel-mac"],
    ["tab-win", "panel-win"],
  ]);
  return (
    <section className="section" id="download" aria-labelledby="download-title">
      <div className="wrap">
        <div className="dl-panel-wrap rv">
          <div className="glow glow-c" aria-hidden="true" />
          <div className="dl-text">
            <p className="eyebrow">{t("dlEyebrow")}</p>
            <h2 id="download-title">{tf("dlTitle", { version: site.version })}</h2>
            <p className="body">{t("dlBody")}</p>
            <div className="actions">
              <a className="btn white" href={repoURL}>
                <GitHubMark size={16} />
                {t("dlGitHub")}
              </a>
              <a className="btn outline" href={releasesURL}>
                {t("dlAll")}
              </a>
            </div>
            {lang.code !== "zh-CN" && <p className="fine">{t("dlLang")}</p>}
          </div>
          <div className="dl">
            <div className="seg" role="tablist" aria-label={t("dlTabsLabel")}>
              <SlidingPill index={tabs.selected} items='[role="tab"]' className="rounded-[9px] bg-white shadow-[0_2px_8px_rgba(0,0,0,0.3)]" />
              <button {...tabs.tab(0)}>macOS</button>
              <button {...tabs.tab(1)}>Windows</button>
            </div>
            <div {...tabs.stack}>
              <div className="dl-tab" {...tabs.panel(0)}>
                <p>{t("dlMacBody")}</p>
                <p className="buttons">
                  <a className="btn primary" href={file(macDisk, "arm64")}>
                    <AppleMark size={16} />
                    {t("dlMacButton")}
                  </a>
                  <a className="btn soft" href={file(macDisk, "amd64")}>
                    {t("dlMacIntel")}
                  </a>
                </p>
                <p>{t("dlMacScript")}</p>
                <CodeBlock text={macInstallCommand} />
              </div>
              <div className="dl-tab" {...tabs.panel(1)}>
                <p>{t("dlWinBody")}</p>
                <p className="buttons">
                  <a className="btn primary" href={file(windowsSetup, "amd64")}>
                    <WindowsMark size={15} />
                    {t("dlWinButton")}
                  </a>
                  <a className="btn soft" href={file(windowsSetup, "arm64")}>
                    {t("dlWinArm")}
                  </a>
                </p>
                <p className="note">{t("dlWinUnsigned")}</p>
                <p>{t("dlWinScript")}</p>
                <CodeBlock text={windowsInstallCommand} prompt="PS>" />
                <p className="fine">{t("dlWinLimits")}</p>
              </div>
            </div>
            <p className="dl-foot">
              <a href={releaseFileURL(site.version, checksumsFile)}>{t("dlChecksums")}</a>
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
