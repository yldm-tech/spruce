// A language's home page, the landing page.
import { useRef } from "react";
import { useReveals } from "../common/motion";
import { Cta } from "../cta/Cta";
import { Details } from "../details/Details";
import { Developers } from "../developers/Developers";
import { Download } from "../download/Download";
import { Faq } from "../faq/Faq";
import { Features } from "../features/Features";
import { Footer } from "../footer/Footer";
import { Header } from "../header/Header";
import { Hero } from "../hero/Hero";
import { Safety } from "../safety/Safety";
import { Steps } from "../steps/Steps";

export function HomePage() {
  const main = useRef<HTMLElement>(null);
  useReveals(main);
  return (
    <>
      <Header page="home" />
      <main id="main" ref={main}>
        <Hero />
        <Steps />
        <Features />
        <Developers />
        <Details />
        <Safety />
        <Download />
        <Faq />
        <Cta />
      </main>
      <Footer page="home" />
    </>
  );
}
