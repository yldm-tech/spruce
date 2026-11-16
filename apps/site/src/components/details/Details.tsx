// Also in Spruce: the health view, then a card for each smaller feature, each with an icon on the tile gradient of its page in the app (apps/spruce/internal/ui/kit/tone).
import type { ReactNode } from "react";
import { useWords, type ContentKey } from "../../i18n/content";
import { Shot } from "../common/Shot";

// The icons are drawn with strokes on the tile, as the app's page icons are.
const icons: Record<string, ReactNode> = {
  junk: <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6" />,
  maintenance: <path d="M13 2 4 14h7l-1 8 9-12h-7l1-8Z" />,
  health: <path d="M3 12h4l2-6 4 12 2-6h6" />,
  history: <path d="M3 12a9 9 0 1 0 3-6.7M3 4v5h5M12 7v5l3 3" />,
  exclusions: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m5.6 5.6 12.8 12.8" />
    </>
  ),
  low: <path d="M6 16V11a6 6 0 1 1 12 0v5l2 2H4l2-2ZM10 21h4" />,
};

const cards: readonly { icon: keyof typeof icons; tone: string; title: ContentKey; body: ContentKey }[] = [
  { icon: "junk", tone: "green", title: "detailJunkTitle", body: "detailJunkBody" },
  { icon: "maintenance", tone: "amber", title: "detailMaintTitle", body: "detailMaintBody" },
  { icon: "health", tone: "red", title: "detailHealthTitle", body: "detailHealthBody" },
  { icon: "history", tone: "slate", title: "detailHistoryTitle", body: "detailHistoryBody" },
  { icon: "exclusions", tone: "graphite", title: "detailExclTitle", body: "detailExclBody" },
  { icon: "low", tone: "pink", title: "detailLowTitle", body: "detailLowBody" },
];

// shotSizes is the health view's width: the whole page width up to 960 pixels.
const shotSizes = "(min-width: 1016px) 960px, calc(100vw - 40px)";

export function Details() {
  const { t } = useWords();
  return (
    <section className="section" id="details" aria-labelledby="details-title">
      <div className="wrap">
        <header className="head rv">
          <p className="eyebrow">{t("detailsEyebrow")}</p>
          <h2 id="details-title">{t("detailsTitle")}</h2>
        </header>
        <figure className="wide-shot rv">
          <div className="win">
            <Shot name="health-idle" alt={t("shotHealthAlt")} sizes={shotSizes} />
          </div>
        </figure>
        <ul className="details">
          {cards.map((card) => (
            <li key={card.title} className="card detail rv">
              <span className={"ic tone-" + card.tone} aria-hidden="true">
                <svg viewBox="0 0 24 24" width="20" height="20">
                  {icons[card.icon]}
                </svg>
              </span>
              <h3>{t(card.title)}</h3>
              <p>{t(card.body)}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
