import SessionTabSync from "./SessionTabSync";
import URLOpenChooserHost from "../common/URLOpenChooserHost";

export default function AppBridge({
  onNewTab,
}: {
  onNewTab?: (id: string, project?: string) => void;
}) {
  return (
    <>
      <SessionTabSync onNewTab={onNewTab} />
      <URLOpenChooserHost />
    </>
  );
}
