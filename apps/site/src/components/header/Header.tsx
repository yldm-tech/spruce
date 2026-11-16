// The header: the brand, the section links in a pill that marks the section being read, the language menu and the GitHub link.
import { useEffect, useState } from "react";
import { useWords, type ContentKey } from "../../i18n/content";
import { pagePath, repoURL } from "../../lib/site";
import { Brand } from "../common/Brand";
import { GitHubMark } from "../common/marks";
import { SlidingPill } from "../common/SlidingPill";
import { LangLinks } from "./LangLinks";

// sections are the header's links in the page's order, so the pill moves left to right as the page scrolls down.
const sections: readonly [string, ContentKey][] = [
  ["features", "navFeatures"],
  ["developers", "navDevelopers"],
  ["safety", "navSafety"],
  ["download", "navDownload"],
  ["faq", "navFaq"],
];

// Header is the page's header; 404.html, which has no language, leaves the section links out.
export function Header({ page }: { page: "home" | "404" }) {
  const { t, lang } = useWords();
  const [scrolled, setScrolled] = useState(false);
  const [current, setCurrent] = useState("");

  // The header turns solid once the page scrolls, and on the home page its pill marks the section being read.
  useEffect(() => {
    let pending = false;
    const update = () => {
      pending = false;
      setScrolled(window.scrollY > 12);
      if (page !== "home") {
        return;
      }
      // The section being read is the lowest one whose top has passed 40% of the window: by where the sections are on the page, not the order of the links, so a section placed after another is never shadowed by it.
      let now = "";
      let lowest = -Infinity;
      for (const [id] of sections) {
        const top = document.getElementById(id)?.getBoundingClientRect().top;
        if (top !== undefined && top < window.innerHeight * 0.4 && top > lowest) {
          lowest = top;
          now = id;
        }
      }
      setCurrent(now);
    };
    const onScroll = () => {
      if (!pending) {
        pending = true;
        window.requestAnimationFrame(update);
      }
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    update();
    return () => window.removeEventListener("scroll", onScroll);
  }, [page]);

  return (
    <>
      <a className="skip" href="#main">
        {t("skipToContent")}
      </a>
      <header className={"top" + (scrolled ? " scrolled" : "")}>
        <div className="wrap top-in">
          <a className="brand" href={pagePath(lang.code)} aria-label={t("homeLabel")}>
            <Brand first />
          </a>
          {page === "home" && (
            <nav className="nav" aria-label={t("navLabel")}>
              <SlidingPill
                index={sections.findIndex(([id]) => id === current)}
                items="a"
                className="rounded-[16px] bg-white/14 shadow-[inset_0_0_0_1px_rgba(255,255,255,0.12)]"
              />
              {sections.map(([id, key]) => (
                <a key={id} href={"#" + id} className={current === id ? "on" : undefined}>
                  {t(key)}
                </a>
              ))}
            </nav>
          )}
          <div className="top-end">
            <details className="lang">
              <summary aria-label={t("languageLabel") + ": " + lang.name}>
                <svg className="i" viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                  <circle cx="12" cy="12" r="9" />
                  <path d="M3 12h18M12 3c2.6 2.8 2.6 15.2 0 18M12 3c-2.6 2.8-2.6 15.2 0 18" />
                </svg>
                <span className="lang-name">{lang.name}</span>
                <svg className="i chev" viewBox="0 0 24 24" width="12" height="12" aria-hidden="true">
                  <path d="m6 9 6 6 6-6" />
                </svg>
              </summary>
              <ul className="lang-menu">
                <LangLinks page={page} item />
              </ul>
            </details>
            <a className="gh" href={repoURL} aria-label={t("githubLabel")}>
              <GitHubMark size={16} />
              <span>GitHub</span>
            </a>
          </div>
        </div>
      </header>
    </>
  );
}
