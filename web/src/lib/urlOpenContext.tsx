import { createContext, useContext, useMemo, type ReactNode } from "react";
import type { URLOpenOwner } from "./externalLinks";

const URLOpenOwnerContext = createContext<URLOpenOwner | null>(null);

export function URLOpenOwnerProvider({
  projectPath,
  host,
  children,
}: URLOpenOwner & { children: ReactNode }) {
  const value = useMemo(
    () => (projectPath ? { projectPath, host } : null),
    [projectPath, host],
  );
  return (
    <URLOpenOwnerContext.Provider value={value}>
      {children}
    </URLOpenOwnerContext.Provider>
  );
}

export function useURLOpenOwner(): URLOpenOwner | null {
  return useContext(URLOpenOwnerContext);
}
