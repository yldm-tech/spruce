import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

/** Whether a page's modal overlay covers the window, so the app shell (the sidebar) and the page under the overlay must be inert: the overlays are drawn inside the main area only, but their modality must hold for the keyboard and assistive technology everywhere. */
interface ShellBlock {
  blocked: boolean;
  /** acquire blocks the shell until the returned release is called; several overlays may hold it at once. */
  acquire: () => () => void;
}

/** Outside a ShellBlockProvider (a page rendered on its own, as in tests) there is no shell to block. */
const ShellBlockContext = createContext<ShellBlock>({ blocked: false, acquire: () => () => {} });

/** ShellBlockProvider holds the blocked state for the app shell: blocked while any overlay holds it. */
export function ShellBlockProvider({ children }: { children: ReactNode }) {
  const [holders, setHolders] = useState(0);
  const acquire = useCallback(() => {
    setHolders((n) => n + 1);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      setHolders((n) => n - 1);
    };
  }, []);
  const blocked = holders > 0;
  const value = useMemo(() => ({ blocked, acquire }), [blocked, acquire]);
  return <ShellBlockContext.Provider value={value}>{children}</ShellBlockContext.Provider>;
}

/** useShellBlocked tells the shell whether it must be inert. */
export function useShellBlocked(): boolean {
  return useContext(ShellBlockContext).blocked;
}

/** useBlockShell makes the shell inert while active is true and releases it when the calling component unmounts. */
export function useBlockShell(active: boolean) {
  const { acquire } = useContext(ShellBlockContext);
  useEffect(() => (active ? acquire() : undefined), [active, acquire]);
}
