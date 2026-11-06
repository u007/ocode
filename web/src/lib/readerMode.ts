export interface ReaderModeInput {
  /** Side pane state key of the focused surface (null when nothing is focused). */
  sideStateKey: string | null;
  sidePanelKind: "chat" | "term";
  browserOpen: boolean;
  collapsed?: boolean;
  reader?: boolean;
  sidePaneVisible: boolean;
  isMobile: boolean;
}

/**
 * Whether the focused chat session shows reader mode: the preview fills the
 * main area and the chat becomes a narrow right-hand column. Reader mode is a
 * desktop layout over the chat side pane, so it needs that pane to be open,
 * expanded and allowed to render on the current sub-tab.
 */
export function shouldRenderReaderPreview({
  sideStateKey,
  sidePanelKind,
  browserOpen,
  collapsed,
  reader,
  sidePaneVisible,
  isMobile,
}: ReaderModeInput): boolean {
  return !!sideStateKey && sidePanelKind === "chat" && browserOpen && !collapsed && !!reader && sidePaneVisible && !isMobile;
}
