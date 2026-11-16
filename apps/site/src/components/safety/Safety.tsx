// Safety: the five rules Spruce keeps before it moves or deletes anything, and where its data stays.
import { Rich, useWords, type ContentKey } from "../../i18n/content";

const rules: readonly [ContentKey, ContentKey][] = [
  ["safety1Title", "safety1Body"],
  ["safety2Title", "safety2Body"],
  ["safety3Title", "safety3Body"],
  ["safety4Title", "safety4Body"],
  ["safety5Title", "safety5Body"],
];

export function Safety() {
  const { t } = useWords();
  return (
    <section className="section" id="safety" aria-labelledby="safety-title">
      <div className="wrap">
        <header className="head rv">
          <p className="eyebrow">{t("safetyEyebrow")}</p>
          <h2 id="safety-title">{t("safetyTitle")}</h2>
        </header>
        <ul className="rules">
          {rules.map(([title, body]) => (
            <li key={title} className="card rule rv">
              <span className="tick" aria-hidden="true">
                <svg viewBox="0 0 24 24" width="14" height="14">
                  <path d="m5 12.5 4.5 4.5L19 7.5" />
                </svg>
              </span>
              <h3>{t(title)}</h3>
              <p>
                <Rich text={t(body)} />
              </p>
            </li>
          ))}
          <li className="card rule privacy rv" id="privacy">
            <span className="tick" aria-hidden="true">
              <svg viewBox="0 0 24 24" width="14" height="14">
                <path d="M7 11V8a5 5 0 0 1 10 0v3M5 11h14v10H5z" />
              </svg>
            </span>
            <h3>{t("privacyTitle")}</h3>
            <p>{t("privacyBody")}</p>
          </li>
        </ul>
      </div>
    </section>
  );
}
