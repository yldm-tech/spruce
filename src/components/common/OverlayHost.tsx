import { createContext, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

const OverlayHostContext = createContext<HTMLElement | null>(null);

/** OverlayHost is where page overlays (GlassDialog and the views built on it) are drawn: a layer over the main area, outside the page's own tree, so a dialog stays usable while the page under it is inert, wherever the page renders it. The root layout puts it inside .app-main. */
export function OverlayHost({ children }: { children: ReactNode }) {
  const [host, setHost] = useState<HTMLElement | null>(null);
  return (
    <OverlayHostContext.Provider value={host}>
      {children}
      <div ref={setHost} className="overlay-host" />
    </OverlayHostContext.Provider>
  );
}

/** InOverlayHost draws children in the OverlayHost, or in place where there is none (a page rendered on its own, as in tests). */
export function InOverlayHost({ children }: { children: ReactNode }) {
  const host = useContext(OverlayHostContext);
  return host ? createPortal(children, host) : children;
}
