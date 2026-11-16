// The hero: the headline, the Download button, which downloads the disk image for a Mac with Apple silicon (the download section has the others), and the overview in a window.
import site from "virtual:spruce/site";
import { Headline, useWords } from "../../i18n/content";
import { macDisk, releaseFileURL } from "../../lib/site";
import { AppleMark } from "../common/marks";
import { Shot } from "../common/Shot";

// shotSizes is the screenshot's width in the layout: the stage's 1120 pixels at most, less the window's frame, or the width of the page below its padding.
const shotSizes = "(min-width: 1176px) 1118px, calc(100vw - 42px)";

export function Hero() {
  const { t, tf } = useWords();
  return (
    <section className="hero" aria-labelledby="hero-title">
      <div className="hero-sky" aria-hidden="true">
        <span className="blob blob-a" />
        <span className="blob blob-b" />
      </div>
      <div className="wrap hero-in">
        <div data-enter="">
          <img className="hero-icon" src="/logo.png" width="112" height="112" alt="" />
        </div>
        <h1 id="hero-title" data-enter="">
          <Headline title={t("heroTitle")} mark={t("heroTitleMark")} />
        </h1>
        <p className="lead" data-enter="">
          {t("heroLead")}
        </p>
        <div className="actions" data-enter="">
          <a className="btn primary big" href={releaseFileURL(site.version, macDisk(site.version, "arm64"))}>
            <AppleMark size={18} />
            {t("heroDownloadMac")}
            <span className="shine" aria-hidden="true" />
          </a>
          <a className="btn ghost big" href="#download">
            {t("heroOtherSystems")}
          </a>
        </div>
        <p className="meta" data-enter="">
          {tf("heroMeta", { version: site.version })}
        </p>
        <figure className="stage" data-enter="stage">
          <div className="stage-glow" aria-hidden="true" />
          <div className="win">
            <div className="win-bar" aria-hidden="true">
              <span className="lights">
                <i />
                <i />
                <i />
              </span>
            </div>
            <Shot name="overview-ready" alt={t("shotOverviewAlt")} sizes={shotSizes} />
          </div>
        </figure>
      </div>
    </section>
  );
}
