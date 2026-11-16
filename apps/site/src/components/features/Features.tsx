// What Spruce does: a row for each feature, its text beside a screenshot of it, the sides alternating; then the menu bar, which has no screenshot, with its two icons.
import site from "virtual:spruce/site";
import { Rich, useWords, type ContentKey } from "../../i18n/content";
import type { ShotName } from "../../lib/site";
import { Shot } from "../common/Shot";

// shotSizes is a row's screenshot width in the layout: beside the text from 912 pixels, up to 640 pixels at the widest wrap, and the width of the page below its padding when it wraps under the text.
export const rowShotSizes = "(min-width: 1176px) 640px, (min-width: 912px) 56vw, calc(100vw - 40px)";

interface Row {
  id: string;
  eyebrow: ContentKey;
  title: ContentKey;
  body: ContentKey;
  points?: readonly ContentKey[];
  shot: ShotName;
  alt: ContentKey;
}

const rows: readonly Row[] = [
  { id: "recommendations", eyebrow: "recEyebrow", title: "recTitle", body: "recBody", shot: "overview-toast-undo", alt: "shotToastAlt" },
  { id: "undo", eyebrow: "undoEyebrow", title: "undoTitle", body: "undoBody", shot: "history-restore", alt: "shotHistoryAlt" },
  { id: "space-lens", eyebrow: "spaceEyebrow", title: "spaceTitle", body: "spaceBody", shot: "space-results", alt: "shotSpaceAlt" },
  { id: "growth", eyebrow: "growthEyebrow", title: "growthTitle", body: "growthBody", shot: "space-growth-7d", alt: "shotGrowthAlt" },
  { id: "uninstall", eyebrow: "uninstallEyebrow", title: "uninstallTitle", body: "uninstallBody", shot: "uninstall-detail", alt: "shotUninstallAlt" },
  {
    id: "automatic",
    eyebrow: "autoEyebrow",
    title: "autoTitle",
    body: "autoBody",
    points: ["autoPoint1", "autoPoint2"],
    shot: "settings-auto-on",
    alt: "shotAutoAlt",
  },
];

export function Features() {
  const { t } = useWords();
  return (
    <section className="section" id="features" aria-labelledby="features-title">
      <div className="wrap">
        <header className="head rv">
          <p className="eyebrow">{t("featuresEyebrow")}</p>
          <h2 id="features-title">{t("featuresTitle")}</h2>
        </header>
        {rows.map((row, i) => (
          <article key={row.id} id={row.id} className={"split-in feature" + (i % 2 === 1 ? " flip" : "")} aria-labelledby={row.id + "-title"}>
            <div className="split-text rv">
              <p className="eyebrow">{t(row.eyebrow)}</p>
              <h3 id={row.id + "-title"}>{t(row.title)}</h3>
              <p className="body">
                <Rich text={t(row.body)} />
              </p>
              {row.points && (
                <ul className="points">
                  {row.points.map((p) => (
                    <li key={p}>{t(p)}</li>
                  ))}
                </ul>
              )}
            </div>
            <figure className="split-media rv">
              <div className="win">
                <Shot name={row.shot} alt={t(row.alt)} sizes={rowShotSizes} />
              </div>
            </figure>
          </article>
        ))}
        <article className="card menubar rv" id="menu-bar" aria-labelledby="menu-bar-title">
          <div className="tray">
            <img src={site.tray.normal.src} width={site.tray.normal.w} height={site.tray.normal.h} alt={t("trayNormalAlt")} loading="lazy" decoding="async" />
            <img src={site.tray.alert.src} width={site.tray.alert.w} height={site.tray.alert.h} alt={t("trayAlertAlt")} loading="lazy" decoding="async" />
          </div>
          <div>
            <h3 id="menu-bar-title">{t("menubarTitle")}</h3>
            <p>{t("menubarBody")}</p>
          </div>
        </article>
      </div>
    </section>
  );
}
