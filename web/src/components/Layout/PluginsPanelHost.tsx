import { useImperativeHandle, useState, type Ref } from "react";
import PluginsPanel from "./PluginsPanel";

export interface PluginsPanelHostHandle {
  open: () => void;
}

/** Owns the dialog's open state so opening or closing it re-renders only the
 *  dialog, not the component that mounts it. */
export function PluginsPanelHost({ ref }: { ref: Ref<PluginsPanelHostHandle> }) {
  const [open, setOpen] = useState(false);
  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), []);
  return <PluginsPanel open={open} onOpenChange={setOpen} />;
}
