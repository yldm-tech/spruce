// For developers: the projects view beside its text, then the command line with a terminal of three of its commands (README.md, Command line).
import { Fragment } from "react";
import { Rich, useWords, type ContentKey } from "../../i18n/content";
import { readmeURL } from "../../lib/site";
import { Shot } from "../common/Shot";
import { rowShotSizes } from "../features/Features";

// commands are the terminal's lines: a comment, then the command after the prompt, each word coloured as the design's terminals are.
const commands: readonly { comment: ContentKey; words: readonly [string, string][] }[] = [
  {
    comment: "cliCommentScan",
    words: [
      ["k", "spruce"],
      ["s", "recommendations"],
      ["f", "--json"],
    ],
  },
  {
    comment: "cliCommentDry",
    words: [
      ["k", "spruce"],
      ["s", "clean"],
      ["f", "--dry-run"],
    ],
  },
  {
    comment: "cliCommentProjects",
    words: [
      ["k", "spruce"],
      ["s", "projects"],
    ],
  },
];

export function Developers() {
  const { t, lang } = useWords();
  return (
    <section className="section" id="developers" aria-labelledby="developers-title">
      <div className="wrap">
        <div className="split-in">
          <div className="split-text rv">
            <p className="eyebrow">{t("devEyebrow")}</p>
            <h2 id="developers-title">{t("devTitle")}</h2>
            <p className="body">
              <Rich text={t("devBody")} />
            </p>
            <ul className="points">
              <li>{t("devPoint1")}</li>
              <li>{t("devPoint2")}</li>
              <li>{t("devPoint3")}</li>
            </ul>
          </div>
          <figure className="split-media rv">
            <div className="win">
              <Shot name="dev-projects" alt={t("shotDevAlt")} sizes={rowShotSizes} />
            </div>
          </figure>
        </div>
        <div className="split-in flip cli">
          <div className="split-text rv">
            <h3>{t("cliTitle")}</h3>
            <p className="body">
              <Rich text={t("cliBody")} />
            </p>
            <p className="more">
              <a href={readmeURL(lang) + "#" + lang.cliSection}>
                <Rich text={t("cliReadme")} /> <span aria-hidden="true">→</span>
              </a>
            </p>
          </div>
          <div className="term rv">
            <div className="term-bar" aria-hidden="true">
              <span className="lights">
                <i />
                <i />
                <i />
              </span>
            </div>
            <pre className="term-body">
              <code>
                {commands.map((c, i) => (
                  <Fragment key={c.comment}>
                    {i > 0 && "\n\n"}
                    <span className="c">{"# " + t(c.comment)}</span>
                    {"\n"}
                    <span className="p" aria-hidden="true">
                      {"$ "}
                    </span>
                    {c.words.map(([kind, word], j) => (
                      <Fragment key={j}>
                        {j > 0 && " "}
                        <span className={kind}>{word}</span>
                      </Fragment>
                    ))}
                  </Fragment>
                ))}
              </code>
            </pre>
          </div>
        </div>
      </div>
    </section>
  );
}
