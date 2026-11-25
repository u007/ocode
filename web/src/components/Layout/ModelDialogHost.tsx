import { useCallback, useImperativeHandle, useState, type Ref } from "react";
import ModelDialog, { type ModelDialogTab } from "./ModelDialog";
import { startUiTiming } from "../../lib/debug/uiTiming";

export interface ModelDialogHostHandle {
  open: (purpose?: ModelDialogTab) => void;
}

/** Owns the dialog's open state so opening or closing it re-renders only the
 *  dialog, not the component that mounts it. */
export function ModelDialogHost({
  ref,
  sessionId,
  host,
}: {
  ref: Ref<ModelDialogHostHandle>;
  sessionId?: string;
  host?: string;
}) {
  const [state, setState] = useState<{ open: boolean; purpose: ModelDialogTab }>({ open: false, purpose: "main" });
  useImperativeHandle(
    ref,
    () => ({
      open: (purpose = "main") => {
        startUiTiming("model-dialog-open");
        setState({ open: true, purpose });
      },
    }),
    [],
  );
  const close = useCallback(() => {
    startUiTiming("model-dialog-close");
    setState((s) => ({ ...s, open: false }));
  }, []);
  return <ModelDialog open={state.open} onClose={close} purpose={state.purpose} sessionId={sessionId} host={host} />;
}
