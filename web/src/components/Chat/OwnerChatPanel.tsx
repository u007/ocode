import type { ComponentProps } from "react";
import {
  findProjectPathForTab,
  useProjectStateRef,
} from "../../stores/projectStore";
import { URLOpenOwnerProvider } from "../../lib/urlOpenContext";
import ChatPanel from "./ChatPanel";

export default function OwnerChatPanel(
  props: ComponentProps<typeof ChatPanel>,
) {
  const projectStateRef = useProjectStateRef();
  const projectPath =
    findProjectPathForTab(projectStateRef.current, props.sessionId) ?? "";
  return (
    <URLOpenOwnerProvider projectPath={projectPath} host={props.host}>
      <ChatPanel {...props} />
    </URLOpenOwnerProvider>
  );
}
