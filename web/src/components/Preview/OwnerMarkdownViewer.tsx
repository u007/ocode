import type { ComponentProps } from "react";
import { URLOpenOwnerProvider } from "../../lib/urlOpenContext";
import MarkdownViewer from "./MarkdownViewer";

export default function OwnerMarkdownViewer(
  props: ComponentProps<typeof MarkdownViewer>,
) {
  return (
    <URLOpenOwnerProvider
      projectPath={props.projectRoot ?? ""}
      host={props.projectHost}
    >
      <MarkdownViewer {...props} />
    </URLOpenOwnerProvider>
  );
}
