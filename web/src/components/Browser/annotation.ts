/**
 * Annotation text + screenshot helpers shared by the Chrome canvas surface and
 * the local iframe surface. The annotation is injected into the chat COMPOSER
 * (never sent): the user can edit it before sending.
 */

export interface AnnotationTextParts {
  title: string;
  url: string;
  selector: string | null;
  label: string;
  note: string;
}

/** The block appended to the composer draft. A blank note is allowed (an
 *  annotation can be just "look at this element"), but the anchor always is.
 *
 *  Everything except the note comes from the page, which is untrusted input to
 *  the agent. Page-derived strings are collapsed to ONE line, length-capped and
 *  stripped of backticks, so a hostile `<title>` cannot inject instructions,
 *  a fake `@.ocode/uploads/...` ref, or a line break that reads as its own
 *  message. The selector is wrapped in backticks to mark it as data. */
export function composeAnnotationText(parts: AnnotationTextParts): string {
  const url = pageText(parts.url, 500);
  const target = pageText(parts.title, 160) || url;
  const lines = [`[Browsers note] ${target} — ${url}`];
  const selector = parts.selector ? pageText(parts.selector, 240).replace(/`/g, "") : "";
  const label = pageText(parts.label, 80).replace(/`/g, "");
  if (selector) lines.push(`Element: \`${selector}\``);
  else if (label) lines.push(`Element: \`${label}\``);
  const note = userText(parts.note, 4000);
  if (note) lines.push(`Note: ${note}`);
  return lines.join("\n");
}

/** Page-derived text: one line, no control characters, capped. */
function pageText(value: string, max: number): string {
  return String(value ?? "")
    .replace(/[\u0000-\u001f\u007f]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, max);
}

/** The user's own note keeps its line breaks; only control characters that can
 *  corrupt the composer are dropped, and the length is bounded. */
function userText(value: string, max: number): string {
  return String(value ?? "")
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, "")
    .trim()
    .slice(0, max);
}

/** File name for the uploaded screenshot. Deterministic enough to group a
 *  session's annotations, distinct enough not to overwrite an earlier one. */
export function annotationFileName(at: number = Date.now()): string {
  return `annotation-${new Date(at).toISOString().replace(/[^0-9A-Za-z]/g, "")}.png`;
}

export interface BoxRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

const WHITE = "#ffffff";
const OUTLINE = "#0ea5e9";

/**
 * Crop the live screencast canvas to `rect` (viewport CSS pixels) plus a
 * margin, and draw the same outline the picker showed. The canvas holds raw
 * page pixels at `canvas.width / canvas.clientWidth`, so the crop is scaled
 * from CSS to device pixels. Regions outside the canvas are clamped away; an
 * element partly below the fold is captured as far as it is visible.
 */
export function cropScreencast(canvas: HTMLCanvasElement, rect: BoxRect, padCss = 16): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const scaleX = canvas.width / Math.max(1, canvas.clientWidth);
    const scaleY = canvas.height / Math.max(1, canvas.clientHeight);
    const padX = padCss * scaleX;
    const padY = padCss * scaleY;
    const boxLeft = rect.left * scaleX;
    const boxTop = rect.top * scaleY;
    const boxW = Math.max(1, rect.width * scaleX);
    const boxH = Math.max(1, rect.height * scaleY);

    const sx = Math.max(0, Math.min(canvas.width, boxLeft - padX));
    const sy = Math.max(0, Math.min(canvas.height, boxTop - padY));
    const ex = Math.max(sx + 1, Math.min(canvas.width, boxLeft + boxW + padX));
    const ey = Math.max(sy + 1, Math.min(canvas.height, boxTop + boxH + padY));
    const w = Math.round(ex - sx);
    const h = Math.round(ey - sy);
    if (w <= 0 || h <= 0) {
      reject(new Error("element is outside the visible page"));
      return;
    }

    const out = document.createElement("canvas");
    out.width = w;
    out.height = h;
    const ctx = out.getContext("2d");
    if (!ctx) {
      reject(new Error("canvas is unavailable"));
      return;
    }
    ctx.fillStyle = WHITE;
    ctx.fillRect(0, 0, w, h);
    ctx.drawImage(canvas, sx, sy, w, h, 0, 0, w, h);
    ctx.strokeStyle = OUTLINE;
    ctx.lineWidth = Math.max(2, Math.round(scaleX));
    ctx.strokeRect(
      boxLeft - sx + ctx.lineWidth / 2,
      boxTop - sy + ctx.lineWidth / 2,
      Math.max(1, boxW - ctx.lineWidth),
      Math.max(1, boxH - ctx.lineWidth),
    );
    try {
      out.toBlob((blob) => (blob ? resolve(blob) : reject(new Error("screenshot encoding failed"))), "image/png");
    } catch (err) {
      reject(err instanceof Error ? err : new Error(String(err)));
    }
  });
}

/** Decode a `data:` URL (capture.js's local-mode screenshot) into a Blob. */
export function dataUrlToBlob(dataUrl: string): Blob {
  const comma = dataUrl.indexOf(",");
  if (comma < 0) throw new Error("invalid image data");
  const meta = dataUrl.slice(0, comma);
  const payload = dataUrl.slice(comma + 1);
  const mime = /data:([^;]+)/.exec(meta)?.[1] ?? "image/png";
  if (/;base64$/.test(meta)) {
    const binary = atob(payload);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return new Blob([bytes], { type: mime });
  }
  return new Blob([decodeURIComponent(payload)], { type: mime });
}

/** Gateway event used to hand an annotation to the owning chat. App resolves
 *  the session and calls `ChatInputHandle.addAnnotation` on that composer. */
export const BROWSER_ANNOTATION_EVENT = "ocode:annotation";

/** Payload of `BROWSER_ANNOTATION_EVENT`. The listener sets `accepted` to true
 *  when a chat composer took the annotation; dispatch is synchronous, so the
 *  sender reads it straight after the dispatch. */
export interface BrowserAnnotationDetail {
  stateKey: string;
  text: string;
  image: Blob | null;
  imageName?: string;
  accepted?: boolean;
}

/** State-key prefix of a side-pane chat surface; the rest is the chat tab id. */
export const SIDE_CHAT_STATE_PREFIX = "side:chat:";

export interface AnnotationRoutingInput {
  /** The surface that produced the annotation (`tab:<id>` or `side:chat:<id>`). */
  stateKey: string;
  activeTabId: string | null;
  chatTabs: Array<{ id: string; projectPath: string }>;
  browserTabs: Array<{ projectPath: string; tab: { id: string } }>;
  /** True when a chat composer is registered for this chat tab id. */
  hasComposer: (chatTabId: string) => boolean;
}

/** Which chat tab receives an annotation. Order:
 *  - side pane: the chat it names, and nothing else;
 *  - browser tab in a known project: the active chat when it is in that
 *    project, then any other chat in that project. Never another project;
 *  - browser tab whose project is unknown: the active chat.
 *  The first candidate with a registered composer wins, or null. */
export function resolveAnnotationTarget(input: AnnotationRoutingInput): string | null {
  const { stateKey, activeTabId, chatTabs, browserTabs, hasComposer } = input;
  let candidates: Array<string | null>;
  if (stateKey.startsWith(SIDE_CHAT_STATE_PREFIX)) {
    candidates = [stateKey.slice(SIDE_CHAT_STATE_PREFIX.length) || null];
  } else {
    const projectPath = browserTabs.find(({ tab }) => `tab:${tab.id}` === stateKey)?.projectPath;
    if (projectPath) {
      const activeTab = chatTabs.find((tab) => tab.id === activeTabId);
      candidates = [
        activeTab && activeTab.projectPath === projectPath ? activeTab.id : null,
        ...chatTabs.filter((tab) => tab.projectPath === projectPath).map((tab) => tab.id),
      ];
    } else {
      candidates = [activeTabId];
    }
  }
  return candidates.find((id): id is string => !!id && hasComposer(id)) ?? null;
}

/** Shown in the annotation popover when no chat composer accepted the note. */
export const ANNOTATION_NO_CHAT_ERROR = "No chat is open in this project to receive the annotation.";

/** Hand an annotation to the owning chat composer. Returns whether a composer
 *  accepted it; `false` means nothing received it and the sender must keep its
 *  note open rather than discard it. */
export function dispatchBrowserAnnotation(
  detail: Omit<BrowserAnnotationDetail, "accepted">,
): boolean {
  const payload: BrowserAnnotationDetail = { ...detail, accepted: false };
  window.dispatchEvent(new CustomEvent<BrowserAnnotationDetail>(BROWSER_ANNOTATION_EVENT, { detail: payload }));
  return payload.accepted === true;
}
