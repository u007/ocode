import { renderedSpeechText } from "../components/Speech/speechUtils";

/**
 * Plain text for "copy as it is" from a rendered block.
 *
 * Blocks mark their copyable content with `[data-copy-content]` so the
 * extraction can skip the surrounding control affordances (disclosure toggles,
 * "Show output" buttons). Falling back to the whole subtree keeps plain blocks
 * that need no marking working unchanged. The underlying extractor is the same
 * one the speech path uses, so markdown syntax is already gone.
 */
export function renderedCopyText(root: Node | null | undefined): string {
  if (!root) return "";
  const scope = root as ParentNode;
  if (typeof scope.querySelectorAll !== "function") return renderedSpeechText(root);
  const nodes = Array.from(scope.querySelectorAll("[data-copy-content]"));
  const parts = (nodes.length > 0 ? nodes : [root])
    .map((node) => renderedSpeechText(node))
    .filter(Boolean);
  return parts.join("\n");
}
