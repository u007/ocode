import { useCallback, useEffect, useRef, useState } from "react";
import { useProjectActions, useProjectState } from "../../stores/projectStore";
import { useBrowserTabs } from "../../stores/browserTabsStore";
import { browserActions } from "../../lib/browserStore";
import {
  OPEN_URL_EVENT,
  isHTTPURL,
  openExternalURL,
  type OpenURLDetail,
} from "../../lib/externalLinks";
import { tabFocusActions } from "../../lib/tabFocus";
import URLOpenDialog from "./URLOpenDialog";

/** Sole owner of the embedded-vs-external URL chooser. */
export default function URLOpenChooserHost() {
  const { state: projectState } = useProjectState();
  const { selectProject } = useProjectActions();
  const activeProjectPath = projectState.activeProject?.path ?? "";
  const [pendingURL, setPendingURL] = useState<OpenURLDetail | null>(null);
  const pendingURLRef = useRef<OpenURLDetail | null>(null);
  const { openBrowserTab: openOwnerBrowserTab } = useBrowserTabs(
    pendingURL?.projectPath ?? activeProjectPath,
  );

  const clearPendingURL = useCallback(() => {
    pendingURLRef.current = null;
    setPendingURL(null);
  }, []);

  useEffect(() => {
    const onOpenURL = (event: Event) => {
      const detail = (event as CustomEvent<OpenURLDetail>).detail;
      if (!detail || typeof detail.url !== "string" || !isHTTPURL(detail.url))
        return;
      event.preventDefault();
      if (pendingURLRef.current) {
        detail.refused = true;
        return;
      }
      pendingURLRef.current = detail;
      setPendingURL(detail);
    };
    window.addEventListener(OPEN_URL_EVENT, onOpenURL);
    return () => window.removeEventListener(OPEN_URL_EVENT, onOpenURL);
  }, []);

  const openURLInBrowserTab = useCallback(() => {
    const request = pendingURLRef.current;
    if (!request) return;
    const project = request.projectPath
      ? projectState.projects.find(
          (p) =>
            p.path === request.projectPath &&
            (p.host ?? "") === (request.host ?? ""),
        )
      : projectState.activeProject;
    if (!project) return;
    const id = openOwnerBrowserTab();
    browserActions.open(`tab:${id}`, request.url);
    clearPendingURL();
    if (
      (projectState.activeProject?.path ?? "") !== project.path ||
      (projectState.activeProject?.host ?? "") !== (project.host ?? "")
    ) {
      void selectProject(project);
    }
    tabFocusActions.request({
      kind: "browser",
      projectPath: project.path,
      host: project.host,
    });
  }, [
    clearPendingURL,
    openOwnerBrowserTab,
    projectState.activeProject,
    projectState.projects,
    selectProject,
  ]);

  const openPendingURLExternal = useCallback(() => {
    const request = pendingURLRef.current;
    if (!request) return;
    openExternalURL(request.url);
    clearPendingURL();
  }, [clearPendingURL]);

  return (
    <URLOpenDialog
      open={pendingURL !== null}
      url={pendingURL?.url ?? ""}
      canOpenInTab={
        pendingURL !== null &&
        (pendingURL.projectPath
          ? projectState.projects.some(
              (p) =>
                p.path === pendingURL.projectPath &&
                (p.host ?? "") === (pendingURL.host ?? ""),
            )
          : activeProjectPath !== "")
      }
      onOpenInTab={openURLInBrowserTab}
      onOpenExternal={openPendingURLExternal}
      onCancel={clearPendingURL}
    />
  );
}
