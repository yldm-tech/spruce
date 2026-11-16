// The closing call to download: the disk image for a Mac with Apple silicon, as the hero's button, and a link to the other downloads.
import site from "virtual:spruce/site";
import { useWords } from "../../i18n/content";
import { macDisk, releaseFileURL } from "../../lib/site";
import { AppleMark } from "../common/marks";

export function Cta() {
  const { t } = useWords();
  return (
    <section className="cta" aria-labelledby="cta-title">
      <div className="cta-sky" aria-hidden="true">
        <span className="blob blob-d" />
        <span className="blob blob-e" />
      </div>
      <div className="wrap cta-in rv">
        <img className="cta-icon" src="/logo.png" width="96" height="96" alt="" />
        <h2 id="cta-title">{t("ctaTitle")}</h2>
        <p className="lead">{t("ctaLead")}</p>
        <p>
          <a className="btn primary huge" href={releaseFileURL(site.version, macDisk(site.version, "arm64"))}>
            <AppleMark size={18} />
            {t("heroDownloadMac")}
            <span className="shine" aria-hidden="true" />
          </a>
        </p>
        <p className="fine">
          {t("ctaNote") + " "}
          <a href="#download">{t("ctaOther")}</a>
        </p>
      </div>
    </section>
  );
}
