// LangLinks are the links to the page in each language, as the header's menu and the footer list them, the page's own marked as the current page. 404.html is no language's page, so it marks none.
import site from "virtual:spruce/site";
import { useWords } from "../../i18n/content";
import { languages, pagePath } from "../../lib/site";

export function LangLinks({ page, item = false }: { page: "home" | "404"; item?: boolean }) {
  const { lang } = useWords();
  const langs = languages.filter((l) => l.code in site.og);
  return (
    <>
      {langs.map((l) => {
        const link = (
          <a
            key={l.code}
            href={pagePath(l.code)}
            hrefLang={l.tag}
            lang={l.tag}
            data-lang={l.code}
            aria-current={page !== "404" && l.code === lang.code ? "page" : undefined}
          >
            {l.name}
          </a>
        );
        return item ? <li key={l.code}>{link}</li> : link;
      })}
    </>
  );
}
