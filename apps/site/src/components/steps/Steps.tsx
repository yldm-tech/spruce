// How it works: it checks on its own, explains each cleanup, and acts on one button.
import { useWords, type ContentKey } from "../../i18n/content";

const steps: readonly { title: ContentKey; body: ContentKey }[] = [
  { title: "stepCheckTitle", body: "stepCheckBody" },
  { title: "stepExplainTitle", body: "stepExplainBody" },
  { title: "stepActTitle", body: "stepActBody" },
];

export function Steps() {
  const { t } = useWords();
  return (
    <section className="section" id="how" aria-labelledby="how-title">
      <div className="wrap">
        <header className="head rv">
          <p className="eyebrow">{t("stepsEyebrow")}</p>
          <h2 id="how-title">{t("stepsTitle")}</h2>
        </header>
        <ol className="steps">
          {steps.map((step, i) => (
            <li key={step.title} className="card step rv">
              <span className="n" aria-hidden="true">
                {i + 1}
              </span>
              <h3>{t(step.title)}</h3>
              <p>{t(step.body)}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
