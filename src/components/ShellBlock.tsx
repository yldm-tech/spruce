import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

/** Whether a page's modal overlay covers the window, so the app shell (the sidebar) must be inert: the overlays are drawn inside the main area only, but their modality must hold for the keyboard and assistive technology everywhere. */
interface ShellBlock {
  blocked: boolean;
  setBlocked: (blocked: boolean) => void;
}

/** Outside a ShellBlockProvider (a page rendered on its own, as in tests) there is no shell to block. */
const ShellBlockContext = createContext<ShellBlock>({ blocked: false, setBlocked: () => {} });

/** ShellBlockProvider holds the blocked state for the app shell. */
export function ShellBlockProvider({ children }: { children: ReactNode }) {
  const [blocked, setBlocked] = useState(false);
  const value = useMemo(() => ({ blocked, setBlocked }), [blocked]);
  return <ShellBlockContext.Provider value={value}>{children}</ShellBlockContext.Provider>;
}

/** useShellBlocked tells the shell whether it must be inert. */
export function useShellBlocked(): boolean {
  return useContext(ShellBlockContext).blocked;
}

/** useBlockShell makes the shell inert while active is true and releases it when the calling component unmounts. */
export function useBlockShell(active: boolean) {
  const { setBlocked } = useContext(ShellBlockContext);
  useEffect(() => {
    setBlocked(active);
    return () => setBlocked(false);
  }, [active, setBlocked]);
}
