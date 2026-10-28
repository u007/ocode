import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Copy,
  ClipboardPaste,
  CopyPlus,
  Trash2,
  RotateCcw,
  ArrowUpToLine,
  ArrowDownToLine,
  Search,
  Plus,
  X,
  Volume2,
  Mic,
  MicOff,
  Square,
  Loader2,
} from "lucide-react";
import { FitAddon, Terminal, type Ghostty } from "ghostty-web";
import { registerFileLinkProvider, registerUrlLinkProvider } from "./terminalLinkProvider";
import { openExternalURL } from "../../lib/externalLinks";
import { buildTerminalOptions, TERMINAL_THEME_DEFAULTS } from "./terminalOptions";
import { loadGhostty } from "./ghosttyRuntime";
import { createTerminalStreamScanner, type TerminalStreamScanner } from "./terminalStream";
import { serializeTerminalBuffer } from "./terminalSerialize";
import { createTerminalSearch, type TerminalSearch } from "./terminalSearch";
import {
  clearBuiltinLinkProviders,
  disableBuiltinMouseReporting,
  disableBuiltinSelectionCopy,
  setSelectionAbsolute,
} from "./ghosttyInternals";
import { attachMouseReporting, sendWheelReport } from "./terminalMouse";
import { attachImeHygiene } from "./terminalImeHygiene";
import TerminalFindBar from "./TerminalFindBar";
import { restoreTerminalHistory, TerminalHistoryError } from "./terminalHistory";
import { noteRemoteHostReachable, remoteHostRetryInMs } from "@/lib/remoteHostGate";
import { apiPath, apiWsPath, remoteApiBase, authHeaders, authToken, isRemoteSession } from "@/api/client";
import { loadTerminalBuffer, saveTerminalBuffer } from "./terminalPersistence";
import { createTerminalSnapshot } from "./terminalSnapshot";
import { registerTerminal, unregisterTerminal } from "@/lib/debug/terminalRegistry";
import { playAlertSound } from "./terminalAlertSound";
import { onWake } from "@/lib/wakeSignal";
import { useTerminalState } from "../../stores/terminalStore";
import { registerTerminalFocus, unregisterTerminalFocus } from "./terminalFocus";
import { requestSpeech } from "../Speech/SpeechProvider";
import { sanitizeSpeechText } from "../Speech/speechUtils";
import { writeDesktopClipboard } from "@/lib/wails";
import { reportActionErrorMessage } from "@/lib/actionErrors";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import { useTerminalVoice, voiceUnavailableMessage } from "./useTerminalVoice";

/**
 * Builds the URL and WebSocket subprotocols for /api/terminal/ws. In remote
 * mode the browser can't set an Authorization header and query-string tokens
 * are forbidden (leak vector), so the bearer token travels as a
 * Sec-WebSocket-Protocol entry (`ocode.bearer.<token>`) instead — matching
 * the server-side checkAuth handling added for HandleTerminalWS. In
 * non-remote mode the token stays in the `?token=` query string, unchanged.
 * `project_path` and `terminal_id` are never secrets, so they always go in
 * the query string regardless of mode.
 *
 * Pure function of its inputs (no calls to authToken()/isRemoteSession()
 * internally) so it's trivially unit-testable.
 */
export function buildTerminalWsConnection(opts: {
  token: string;
  projectPath: string | undefined;
  /** Remote project host (`[user@]host` or `wsl:<distro>`); undefined for local. */
  host?: string;
  terminalId: string;
  isRemote: boolean;
  historyOffset?: number;
}): { url: string; protocols: string[] | undefined } {
  const { token, projectPath, host, terminalId, isRemote, historyOffset } = opts;
  const params = new URLSearchParams();
  let protocols: string[] | undefined;
  if (token) {
    if (isRemote) {
      protocols = [`ocode.bearer.${token}`];
    } else {
      params.set("token", token);
    }
  }
  // project_path pins the shell's cwd to this tab's project; the server
  // validates it against its registered project roots. It is also what the
  // local proxy uses to register the project on the host for a remote socket
  // (a WebSocket handshake cannot set the X-Ocode-Project header).
  if (projectPath) params.set("project_path", projectPath);
  params.set("terminal_id", terminalId);
  if (historyOffset !== undefined) params.set("history_offset", String(historyOffset));
  const query = params.toString();
  // A remote (SSH/WSL) project's shell runs inside the host's
  // `ocode serve --remote`, reached through the local server's reverse proxy
  // (/api/remote/{host}/api/...). No host=/port= params: the host would
  // otherwise try to ssh from itself to itself. Local projects keep the bare
  // path so their URLs stay byte-identical.
  const base = host ? remoteApiBase(host) : "";
  // apiWsPath keeps the tailscale --set-path prefix and respects the
  // configured backend origin (same-origin vs hub). Handles ws/wss
  // conversion for absolute backend URLs.
  const url = apiWsPath(`${base}/api/terminal/ws${query ? `?${query}` : ""}`);
  return { url, protocols };
}

/**
 * Keys whose keydown never becomes pty input (see the custom key handler):
 * bare modifiers and lock/layout keys.
 */
const MODIFIER_KEYS = new Set([
  "Shift", "Control", "Alt", "AltGraph", "Meta", "CapsLock", "NumLock", "ScrollLock",
  "Fn", "Hyper", "Super", "OS", "ContextMenu",
]);

// Debounce window for duplicate clipboard writes of the SAME text. In the
// desktop shell one physical Cmd+C can legitimately take two copy paths: the
// native Edit ▸ Copy menu role (bound to CmdOrCtrl+c, firing the WKWebView
// `copy:` selector → a DOM copy event over the terminal selection) AND the page
// keydown handled in attachCustomKeyEventHandler. Both carry identical text,
// so the copy lands twice in clipboard managers (Maccy/Paste history). The
// first writer arms this guard; a second write of the same text within the
// window is a duplicate and is dropped. Different text always writes.
const DUPLICATE_COPY_WINDOW_MS = 250;
let lastCopyText = "";
let lastCopyAt = 0;

type SelectionRange = NonNullable<ReturnType<Terminal["getSelectionPosition"]>>;

/**
 * True when `inner` lies inside `outer` in buffer order. An unknown range
 * (no selection position) is inside only another unknown range, which keeps
 * the pre-existing "same range" behaviour for selections xterm cannot place.
 */
function selectionWithin(inner: SelectionRange | undefined, outer: SelectionRange | undefined): boolean {
  if (!inner || !outer) return !inner && !outer;
  const notAfter = (a: SelectionRange["start"], b: SelectionRange["start"]) => a.y < b.y || (a.y === b.y && a.x <= b.x);
  return notAfter(outer.start, inner.start) && notAfter(inner.end, outer.end);
}

function isDuplicateCopy(text: string): boolean {
  const now = Date.now();
  const dup = text === lastCopyText && now - lastCopyAt < DUPLICATE_COPY_WINDOW_MS;
  if (dup) return true;
  markCopySeen(text);
  return false;
}

/**
 * Records that a clipboard write of `text` just happened (or is happening)
 * without consuming the dedupe guard, so a second copy path for the SAME user
 * action is recognized as a duplicate. Used by the DOM `copy` listener, whose
 * write goes through the event's clipboardData rather than this module's
 * Clipboard-API writer.
 */
function markCopySeen(text: string): void {
  lastCopyText = text;
  lastCopyAt = Date.now();
}

// Debounce window for a duplicate PASTE of the same text, the mirror of
// DUPLICATE_COPY_WINDOW_MS. One physical paste can reach the terminal twice:
// the desktop shell's native Edit ▸ Paste role and the Cmd/Ctrl+V keydown
// default action are separate routes, and only the first is a paste the user
// asked for. Deliberately much tighter than the copy window: the two
// deliveries of one paste land within a few ms, while a longer window would
// start swallowing a user's own rapid repeat (a held/repeated Cmd+V). A
// dropped duplicate is the cheap mistake to make here; a swallowed paste
// reads as "paste is broken".
const DUPLICATE_PASTE_WINDOW_MS = 50;

/** Max base64 chars accepted from an OSC 52 clipboard write (~75 KB of text). */
const OSC52_MAX_PAYLOAD_CHARS = 100_000;
// How long after the user's last press, release or key in THIS terminal an
// OSC 52 write is honoured. Measured from release, and by the page itself,
// because the browser's transient activation (navigator.userActivation) starts
// at mousedown, so a slow drag plus a remote round trip ran it out. WKWebView
// can also leave it unset. Either way every OSC 52 copy was refused.
const OSC52_INPUT_WINDOW_MS = 5_000;

/**
 * Writes text to the system clipboard, falling back to the deprecated
 * execCommand path when the async Clipboard API is unavailable or denied
 * (older WebKit, permission refusal, non-secure context). A same-text write
 * racing another copy path for the same user action (desktop menu Copy +
 * keydown, mouse-up + debounced selection-change) is deduplicated.
 *
 * In the desktop shell the write goes to the native clipboard only: WKWebView
 * refuses both browser paths outside a user gesture, which is exactly where an
 * OSC 52 copy from a remote program arrives.
 */
export async function writeClipboardText(text: string): Promise<void> {
  if (isDuplicateCopy(text)) return;
  if (writeDesktopClipboard(text)) return;
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Capture after the asynchronous rejection: the user may have moved focus
    // since the copy started. Restore that element, not a stale terminal ref.
    const focused = document.activeElement;
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    try {
      ta.focus({ preventScroll: true });
      ta.select();
      if (!document.execCommand("copy")) {
        console.warn("Terminal clipboard copy was rejected");
      }
    } catch (err) {
      console.warn("Terminal clipboard copy failed:", err);
    } finally {
      ta.remove();
      if (focused instanceof HTMLElement && focused.isConnected) {
        focused.focus({ preventScroll: true });
      }
    }
  }
}

/**
 * DECRST for every mouse-tracking mode the emulator can be left in by a TUI
 * (`vim`, `tmux`, `htop`) that enabled it via DECSET and then died before
 * sending the matching disable — which is exactly what happens when a remote
 * SSH tunnel times out or crashes. Mouse modes are emulator parser state, not pty
 * state, so they survive the socket closing. Left on, terminalMouse.ts keeps
 * encoding every mouse move into escape sequences; after a reconnect those are
 * written into the fresh shell, which has no mouse handler and echoes them as
 * literal garbage ("mouse gibberish"). Written via term.write(), so it disables
 * the modes without clearing scrollback the way a full reset (RIS) would.
 * ?1004 is focus reporting (not mouse), so it is deliberately excluded.
 */
const TERMINAL_MOUSE_RESET =
  "\x1b[?9l\x1b[?1000l\x1b[?1001l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?1016l";

/**
 * How long the terminal waits for the REST history restore before giving up and
 * attaching the live socket anyway. `fetch` has no timeout of its own, and the
 * WebSocket is only opened after the restore settles — so a stalled restore
 * (a wedged server, a dropped connection held open by a proxy) would otherwise
 * leave the terminal blank with no output and no error, indistinguishable from
 * a dead shell. Each history page re-arms the timer, so a large-but-progressing
 * restore is never cut off; only a genuinely stalled one is.
 */
const TERMINAL_HISTORY_RESTORE_TIMEOUT_MS = 15000;

/**
 * How often a terminal waiting out a remote host's retry window re-checks the
 * in-memory gate. The gate can close early (a status probe saw the host come
 * back), and waiting the whole multi-minute window would leave the panel dead
 * afterwards. This only reads gate state; it issues no request.
 */
const REMOTE_HOST_RECHECK_MS = 10_000;

/**
 * Upper bound on history bytes replayed into the emulator on (re)open. The on-disk
 * log is unbounded (TUI redraws push a single terminal past 10 MB), and the
 * restore used to grow `scrollback` to fit every row: each page re-allocated
 * the whole scrollback list (O(n²) over the log), the buffer ignored the
 * user's scrollback setting, and the 30s serialized snapshot copied the
 * multi-MB result into localStorage — which is what drove the desktop
 * renderer past 3 GB and hung it. Replaying only the tail keeps the buffer
 * bounded by the user's scrollback; older output stays on disk.
 */
const TERMINAL_HISTORY_RESTORE_MAX_BYTES = 2 * 1024 * 1024;

/**
 * A single interactive terminal: one ghostty-web instance bridged to one
 * pty-backed shell over /api/terminal/ws. Each panel owns its own WebSocket;
 * the server keys the shell by `id`, so a socket drop (reload, remount) only
 * detaches the shell and the next socket with the same id resumes it.
 *
 * `active` mirrors LogPanel's prop: the panel stays mounted while its tab is
 * backgrounded (so the shell keeps running and scrollback survives), but a
 * `display: none` container measures 0x0, so fitting is deferred until the tab
 * is visible again.
 *
 * `id` is also the scrollback persistence key. The server history endpoint is
 * restored oldest-to-newest before the socket opens; the bounded local copy is
 * used only when the endpoint/log is explicitly missing (404), so a restart
 * cannot silently restore only the local tail.
 */
export default function TerminalPanel({
  id,
  active,
  scrollbackLines,
  fontFamily,
  fontSize,
  projectPath,
  host,
}: {
  id: string;
  active: boolean;
  scrollbackLines: number;
  fontFamily: string;
  fontSize: number;
  projectPath: string;
  host?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  // The element ghostty-web opens into. It must be a DEDICATED inner div, never
  // the container: open() makes its host contenteditable and appends the canvas
  // and the hidden input textarea, and the container also holds the find bar's
  // input and the overlays.
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const snapshotRef = useRef<ReturnType<typeof createTerminalSnapshot> | null>(null);
  const searchRef = useRef<TerminalSearch | null>(null);
  const scannerRef = useRef<TerminalStreamScanner | null>(null);
  const resetTerminalRef = useRef<(() => void) | null>(null);
  // Set when libghostty fails to load or the terminal fails to open: shown in
  // the panel instead of a silently blank terminal.
  const [loadError, setLoadError] = useState<string | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dragCounterRef = useRef(0);
  // Drag-guard for links: suppress link activation if mouse moved between
  // mousedown and mouseup (i.e. user dragged a selection, not a plain click).
  const dragStartedRef = useRef(false);
  const dragMovedRef = useRef(false);
  const dragStartXRef = useRef(0);
  const dragStartYRef = useRef(0);
  // Text of the selection as it stood when the user finished selecting.
  // The terminal stores a selection as buffer coordinates, not text: a program that
  // redraws in place (claude code's Ink renderer, especially over a chunked
  // ssh stream) rewrites the rows under those coordinates every frame, so a
  // later getSelection() returns a partial/mutated line set. Every copy path
  // that runs after release (Cmd+C, Edit-menu copy event, context menu)
  // writes this snapshot instead.
  //
  // It deliberately OUTLIVES the terminal's own selection, which can vanish in
  // ways the user never asked for: a scrollback trim that pushes the selected
  // rows off the top (a busy redraw loop does this within seconds), the
  // mouse-button reports a mouse-tracking program (claude code enables
  // ?1000/?1006) receives for the very right-click that opened the context
  // menu, and an alt-screen switch. Only a new left
  // pointer gesture in the terminal, a keystroke that reaches the pty, or
  // Select All replaces or drops it.
  const selectionSnapshotRef = useRef("");
  // Buffer range of the snapshotted selection (see selectionWithin). A
  // right-click whose selection lies INSIDE that range keeps the snapshot:
  // either it is the same drag whose rows were rewritten, or a
  // mouse-tracking program's button report cleared the drag and xterm's
  // rightClickSelectsWord picked one word out of it — the user right-clicked
  // what they had highlighted, so Copy/Play must act on all of it. A
  // selection outside the range is a new one and replaces the snapshot.
  const selectionRangeRef = useRef<SelectionRange | undefined>(undefined);
  // True from a mousedown in the container until the resulting selection has
  // been copied. Gates the debounced onSelectionChange copy so a selection that
  // changes without a pointer gesture (selectAll, a program redrawing under it)
  // never re-copies the now-rewritten rows.
  const userSelectingRef = useRef(false);
  const copyDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; hasSelection: boolean } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement>(null);
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findResult, setFindResult] = useState<{ count: number; index: number }>({ count: 0, index: -1 });
  // Mirrors `active` so the terminal event handlers (registered once at mount) can
  // read the live focused state without re-subscribing.
  const activeRef = useRef(active);
  // Mirrors `scrollbackLines` so the async mount (it awaits the wasm load) builds the
  // terminal with the value current at build time, not the one the render that
  // started the load saw; the live-scrollback effect below only sees an open terminal.
  const scrollbackLinesRef = useRef(scrollbackLines);
  scrollbackLinesRef.current = scrollbackLines;
  // False during the initial scrollback replay; flipped true once the live pty
  // socket opens so a BEL baked into restored history can't false-alert.
  const readyRef = useRef(false);
  // False until the server's attach control frame arrives on the current
  // socket. term.onData must not be forwarded before that: mouse reports left
  // over from a dead session (or keystrokes queued during the handshake) would
  // otherwise be written into the pty before we know whether this is a fresh
  // shell (whose stale mouse modes we must clear) or a resumed one.
  const attachedRef = useRef(false);
  // Reconnection state (refs, not state, to avoid re-renders).
  const reconnectingRef = useRef(false);
  const reconnectAttemptRef = useRef(0);
  const reconnectTimerRef = useRef<number | null>(null);
  // Set true when the close was intentional (user closed the tab or the
  // server confirmed the shell is gone) so we don't try to reconnect.
  const manualCloseRef = useRef(false);
  // Set when the server sent `detached: superseded` — another client took this
  // terminal id's single attachment slot. Ref (not state) so the onclose path
  // and the reconnect backoff can read it without re-rendering mid-teardown;
  // `takenOver` is the rendered form.
  const takeoverRef = useRef(false);
  const [takenOver, setTakenOver] = useState(false);
  const takenOverRef = useRef(false);
  // Rendered form of attachedRef: true once the server's attach frame arrives on
  // the live socket, cleared when that socket goes away. Gates the voice button,
  // since term.paste (like every keystroke) is dropped until the handshake is done.
  const [shellReady, setShellReady] = useState(false);
  /** The effect's connectSocket, reachable from the Take over button (which
   *  renders outside the effect). Set when the socket effect runs. */
  const connectSocketRef = useRef<(() => void) | null>(null);

  const findOpenRef = useRef(findOpen);
  const findQueryRef = useRef(findQuery);
  useEffect(() => { findOpenRef.current = findOpen; }, [findOpen]);
  useEffect(() => { findQueryRef.current = findQuery; }, [findQuery]);

  const { markAlerted, setOscTitle, openTerminal, closeTerminal } = useTerminalState();

  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  // Upload dropped files and insert their names into the terminal stdin.
  const uploadAndInsert = useCallback(async (files: File[]) => {
    if (files.length === 0) return;
    const fd = new FormData();
    files.forEach((f) => fd.append("file", f));
    try {
      const query = projectPath ? `?project=${encodeURIComponent(projectPath)}` : "";
      // A remote project's uploads live on the host, so the POST must go
      // through the remote proxy rather than the local server.
      const r = await fetch(apiPath(`${remoteApiBase(host)}/api/uploads${query}`), {
        method: "POST",
        headers: authHeaders(),
        body: fd,
      });
      if (!r.ok) {
        const body: { error?: string } = await r.json().catch(() => ({}));
        throw new Error(body.error || `upload failed with status ${r.status}`);
      }
      const raw: unknown = await r.json();
      const saved: { name: string }[] = Array.isArray(raw) ? (raw as { name: string }[]) : [];
      if (!Array.isArray(raw)) console.warn("upload response was not an array:", raw);
      const names = saved.map((f) => f.name);
      if (names.length === 0) return;
      const term = termRef.current;
      const sock = socketRef.current;
      if (!term || !sock || sock.readyState !== WebSocket.OPEN) return;
      // Insert uploaded file names into the terminal. Use the relative path
      // under the default upload directory so the user can immediately
      // reference them in shell commands (e.g. `cat .ocode/uploads/foo.txt`).
      // If a custom upload dir is configured this path won't resolve, but
      // the filename alone still helps.
      const paths = names.map((n) => `.ocode/uploads/${n}`);
      const text = paths.join(" ") + " ";
      sock.send(text);
    } catch (err) {
      console.error("terminal: file upload failed:", err);
    }
  }, [projectPath, host]);

  // Voice input: the transcript is pasted as ONE paste, never sent. The send
  // rule is the same as the chat composer's: the user reviews it and presses Return.
  const insertVoiceTranscript = useCallback((text: string) => {
    const term = termRef.current;
    const sock = socketRef.current;
    // Re-checked here, not when recording started: transcription takes seconds
    // and the socket may have dropped meanwhile. term.paste goes through onData,
    // which silently drops input without an attached, open socket.
    if (!term || !attachedRef.current || !sock || sock.readyState !== WebSocket.OPEN) {
      console.warn("terminal: voice transcript not inserted, terminal is not connected");
      reportActionErrorMessage(
        `Terminal is not connected, so the voice transcript was not inserted. Transcript: ${text}`,
      );
      return;
    }
    // Newlines would reach an app without bracketed paste as separate Returns,
    // which runs each line. Keep the transcript on one line. Control characters
    // are removed too: ESC (0x1b), 0x03 and 0x04 would otherwise be interpreted
    // by the shell, and this text is model output, not something the user typed.
    term.paste(text.replace(/\s*[\u0000-\u001f\u007f]+\s*/g, " ").trim());
    term.focus();
  }, []);
  const voice = useTerminalVoice(insertVoiceTranscript);

  // ── Context menu (right-click) — Supacode-style ────────────────
  // A pointer gesture in the terminal proper: left button only (the right
  // button opens the context menu and must NOT copy before the user picks a
  // menu item), and not on the panel's own chrome. The context menu
  // is portaled to <body> but React still bubbles its events through this
  // container, and the find bar / take-over overlay render inside it; a
  // press on any of those leaves the terminal's selection untouched.
  const isTerminalPress = useCallback((e: React.MouseEvent): boolean => {
    if (e.button !== 0) return false;
    const target = e.target as Element;
    if (ctxMenuRef.current?.contains(target)) return false;
    return !target.closest("[data-terminal-chrome]");
  }, []);

  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    // ghostty-web's own contextmenu listener has already run (it never changes
    // the selection): a selection OUTSIDE the snapshotted range is what the user
    // sees under the menu, so snapshot it before anything that follows can clear
    // it. A live selection inside the range snapshotted at mouse-up is that drag,
    // its rows possibly rewritten by a redrawing program since, and the snapshot
    // stays (see selectionRangeRef).
    const term = termRef.current;
    if (term) {
      const live = term.getSelection();
      const range = term.getSelectionPosition();
      if (live && (!selectionSnapshotRef.current || !selectionWithin(range, selectionRangeRef.current))) {
        selectionSnapshotRef.current = live;
        selectionRangeRef.current = range;
      }
    }
    setCtxMenu({ x: e.clientX, y: e.clientY, hasSelection: selectionSnapshotRef.current.length > 0 });
  }, []);

  useEffect(() => {
    if (!ctxMenu) return;
    const onDown = (e: MouseEvent) => {
      if (ctxMenuRef.current && !ctxMenuRef.current.contains(e.target as Node)) setCtxMenu(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setCtxMenu(null);
    };
    // A scroll anywhere on the page moves the terminal out from under the
    // menu, so the menu closes. Scrolls INSIDE the terminal host are not that:
    // ghostty-web's own contextmenu listener parks and focuses its hidden
    // textarea on the same right-click, and that focus scrolls the textarea,
    // whose capture-phase scroll event used to close the menu the instant it
    // opened (a right-click that flashed an empty menu).
    const onScroll = (e: Event) => {
      if (e.target instanceof Node && hostRef.current?.contains(e.target)) return;
      setCtxMenu(null);
    };
    const onResize = () => setCtxMenu(null);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onResize);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onResize);
    };
  }, [ctxMenu]);

  // Text every post-release copy path writes: the snapshot taken when the
  // user finished selecting (or right-clicked), even if the terminal has since
  // dropped the selection on its own. Falls through to a live read only for
  // selections made without a pointer gesture (selectAll, API), which never
  // took a snapshot.
  const selectedTextForCopy = useCallback((): string => {
    const term = termRef.current;
    if (!term) return "";
    return selectionSnapshotRef.current || term.getSelection();
  }, []);

  const handleCopy = useCallback(async () => {
    // The menu closes whether or not there was anything to copy: a Copy that
    // silently left the menu open is how an empty selection used to surface.
    setCtxMenu(null);
    const sel = selectedTextForCopy();
    if (!sel) return;
    await writeClipboardText(sel);
  }, [selectedTextForCopy]);

  const handleSpeakSelection = useCallback(() => {
    const text = sanitizeSpeechText(selectedTextForCopy());
    if (text) requestSpeech(text);
    setCtxMenu(null);
  }, [selectedTextForCopy]);

  const handleSpeakVisible = useCallback(() => {
    setCtxMenu(null);
    const term = termRef.current;
    if (!term) return;
    const buffer = term.buffer.active;
    // buffer.viewportY is always 0 in ghostty-web; derive the first visible
    // absolute row from how far the viewport is scrolled back from the bottom.
    const start = buffer.length - term.rows - Math.floor(term.getViewportY());
    const lines = Array.from({ length: term.rows }, (_, index) => buffer.getLine(start + index)?.translateToString(true) ?? "");
    const text = sanitizeSpeechText(lines.join("\n"));
    if (text) requestSpeech(text);
  }, []);

  const handlePaste = useCallback(async () => {
    // Read the clipboard, but feed it through term.paste like every other
    // paste path: a raw sock.send skips bracketed-paste wrapping (a multiline
    // payload would run line by line in a bracketed-paste app) and bypasses
    // the attach-handshake and socket-state guard term.onData already applies.
    // Not deduped against a clipboard event, and it must not be: this is a
    // context-menu action, not a delivery of the Cmd/Ctrl+V keystroke.
    try {
      const text = await navigator.clipboard.readText();
      if (text) termRef.current?.paste(text);
    } catch {
      // Clipboard read requires a secure context / permission; hint the user.
      // As a fallback we focus the terminal so Ctrl+V / Cmd+V still works.
      termRef.current?.focus();
    }
    setCtxMenu(null);
  }, []);

  // Copy-on-selection (TUI parity): the TUI copies a dragged transcript
  // selection to the system clipboard on mouse release (model.go mouse-up
  // handling calls clipboard.WriteAll). The desktop/web terminal only copied
  // via right-click → Copy, which is why a selection made here never reached
  // the system clipboard. copySelectionNow runs inside the container's
  // mouseup handler so WebKit/WKWebView treats it as a user-gesture clipboard
  // write; the debounced term.onSelectionChange fallback below covers
  // selections whose release lands outside this container and
  // keyboard/programmatic selections.
  const copySelectionNow = useCallback(() => {
    userSelectingRef.current = false;
    if (copyDebounceRef.current !== null) {
      clearTimeout(copyDebounceRef.current);
      copyDebounceRef.current = null;
    }
    const term = termRef.current;
    const sel = term?.getSelection() ?? "";
    if (!term || !sel) return false;
    selectionSnapshotRef.current = sel;
    selectionRangeRef.current = term.getSelectionPosition();
    void writeClipboardText(sel);
    return true;
  }, []);

  const handleSelectAll = useCallback(() => {
    // Not a pointer selection: no snapshot is taken, and the one from an
    // earlier drag must not win over the live read in selectedTextForCopy.
    selectionSnapshotRef.current = "";
    termRef.current?.selectAll();
    setCtxMenu(null);
  }, []);

  const handleClear = useCallback(() => {
    // ghostty-web's clear() erases the screen only; ED 3 drops the scrollback
    // too, which is what xterm's clear() did.
    termRef.current?.clear();
    termRef.current?.write("\x1b[3J");
    // clear is synchronous and not a pty write, so mark the snapshot and the
    // find bar's row cache by hand.
    searchRef.current?.invalidate();
    snapshotRef.current?.markDirty();
    snapshotRef.current?.flush();
    setCtxMenu(null);
  }, []);

  const handleReset = useCallback(() => {
    resetTerminalRef.current?.();
    snapshotRef.current?.flush();
    setCtxMenu(null);
  }, []);

  const handleScrollTop = useCallback(() => {
    termRef.current?.scrollToTop();
    setCtxMenu(null);
  }, []);

  const handleScrollBottom = useCallback(() => {
    termRef.current?.scrollToBottom();
    setCtxMenu(null);
  }, []);

  const handleFind = useCallback(() => {
    const sel = termRef.current?.getSelection() ?? "";
    if (sel && !findOpen) setFindQuery(sel);
    setFindOpen(true);
    // Keep dispatch for backwards compat with any external listener, but the
    // primary find UI is now handled locally via createTerminalSearch.
    window.dispatchEvent(new CustomEvent("ocode:terminal-find", { detail: { id } }));
    setCtxMenu(null);
  }, [id, findOpen]);

  const handleCloseFind = useCallback(() => {
    setFindOpen(false);
    searchRef.current?.clear();
    setFindResult({ count: 0, index: -1 });
    termRef.current?.focus();
  }, []);

  const handleFindNext = useCallback(() => {
    const q = findQuery;
    if (!q.trim()) return;
    searchRef.current?.findNext(q);
  }, [findQuery]);

  const handleFindPrev = useCallback(() => {
    const q = findQuery;
    if (!q.trim()) return;
    searchRef.current?.findPrevious(q);
  }, [findQuery]);

  const handleFindQueryChange = useCallback((q: string) => {
    setFindQuery(q);
    if (!q.trim()) {
      searchRef.current?.clear();
      setFindResult({ count: 0, index: -1 });
      return;
    }
    // Incremental search: select and jump to the first match as you type.
    searchRef.current?.findNext(q);
  }, []);

  const handleNewTerminal = useCallback(() => {
    openTerminal(projectPath, host);
    setCtxMenu(null);
  }, [openTerminal, projectPath, host]);

  const handleCloseTerminal = useCallback(() => {
    // Mark this close as intentional so the WebSocket onclose handler
    // does not attempt to reconnect — the server has been told to kill
    // the shell (DELETE /api/terminal/{id}) and the shell is gone.
    manualCloseRef.current = true;
    closeTerminal(projectPath, id, host);
    setCtxMenu(null);
  }, [closeTerminal, projectPath, host, id]);

  // External find trigger (e.g. from future callers dispatching
  // ocode:terminal-find). HandleFind already opens locally, but this keeps
  // the event useful if dispatched from outside this component.
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail as { id?: string } | undefined;
      if (detail?.id !== id) return;
      const sel = termRef.current?.getSelection() ?? "";
      if (sel && !findOpenRef.current) setFindQuery(sel);
      setFindOpen(true);
    };
    window.addEventListener("ocode:terminal-find", handler as EventListener);
    return () => window.removeEventListener("ocode:terminal-find", handler as EventListener);
  }, [id]);

  // Drag-and-drop handlers — follow the same counter-based pattern used by
  // ChatInput to correctly handle nested dragenter/dragleave from child
  // elements.
  const handleDragEnter = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current++;
    if (e.dataTransfer.types.includes("Files")) {
      setIsDragging(true);
    }
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current--;
    if (dragCounterRef.current === 0) {
      setIsDragging(false);
    }
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      dragCounterRef.current = 0;
      setIsDragging(false);
      const files = Array.from(e.dataTransfer.files);
      if (files.length > 0) {
        uploadAndInsert(files);
      }
    },
    [uploadAndInsert],
  );

  // Fit to the container and tell the pty about the new size. No-op while the
  // container has no layout (hidden tab), which would otherwise force the terminal to
  // a degenerate 1x1 grid and wreck the shell's line wrapping.
  const initialFitDoneRef = useRef(false);
  const fitAndResize = useRef(() => {
    const el = containerRef.current;
    const term = termRef.current;
    const fit = fitRef.current;
    if (!el || !term || !fit) return;
    if (el.clientWidth === 0 || el.clientHeight === 0) return;

    const applyFit = () => {
      fit.fit();
      const sock = socketRef.current;
      if (sock && sock.readyState === WebSocket.OPEN) {
        sock.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    };

    if (initialFitDoneRef.current) {
      applyFit();
      return;
    }

    // On cold launch the surrounding layout (tab bar, sidebar panels) can
    // still be settling when this first fires, so el.clientWidth may be
    // transiently too narrow. Fitting against that reflows a restored
    // buffer's box-drawing content into garbage a later, correctly-sized fit
    // can't undo — so wait for width to hold steady across two frames before
    // the first real fit. ResizeObserver keeps re-invoking this while layout
    // is still settling, so the check naturally retries.
    const w = el.clientWidth;
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (!containerRef.current || containerRef.current.clientWidth !== w) return;
        initialFitDoneRef.current = true;
        applyFit();
      });
    });
  });

  // ── Cmd/Ctrl+C copy & Cmd/Ctrl+V paste ────────────────────────────
  // ghostty-web does not handle copy shortcuts for us. Copy relies on the DOM
  // `copy` event firing over a *browser text selection* — but the selection
  // is canvas-rendered with no DOM Selection, so browsers are not obliged to
  // fire it (WKWebView in the desktop shell doesn't). And its key encoder would
  // turn Ctrl+C into \x03 (SIGINT) on every platform. So the clipboard
  // shortcuts are intercepted explicitly. NOTE the key-handler convention is
  // the inverse of xterm.js: returning true means "handled, skip the
  // terminal AND cancel the browser default", false means "carry on":
  //   - Cmd/Ctrl+C with a selection → copy and return true (which also stops
  //     the \x03). No selection → false, so Ctrl+C still sends SIGINT (on mac
  //     ghostty-web ignores meta+C itself).
  //   - Cmd/Ctrl+V → return false WITHOUT preventDefault. ghostty-web already
  //     skips (ctrl|meta)+KeyV before it would encode the key, and the paste
  //     itself comes from the browser's own `paste` event (see the container
  //     listener below): returning true would preventDefault the keydown and
  //     kill that event, and the keydown must NOT read the clipboard either, or
  //     one Cmd/V pastes twice.
  const copyViaShortcut = useCallback(() => {
    // Gated on the VISIBLE selection, not the snapshot: once nothing is
    // highlighted, Ctrl+C must reach the pty as SIGINT again. The snapshot
    // only decides WHAT is copied while a selection is still showing.
    const term = termRef.current;
    if (!term?.hasSelection()) return false;
    const sel = selectedTextForCopy();
    if (!sel) return false;
    void writeClipboardText(sel);
    return true;
  }, [selectedTextForCopy]);

  // Container-level `copy` listener — the desktop-shell fallback. In the
  // Wails webview the native Edit-menu Copy role can consume Cmd+C before a
  // keydown ever reaches the page; the webview then dispatches a DOM `copy`
  // event instead. This parent-level listener answers it with the selection
  // text (skipping when something already handled it), so a selection is
  // copied whichever path the OS event took. The markCopySeen
  // bookkeeping is what stops the double copy: it records EVERY copy of a
  // selection passing through this container — including events another
  // listener answered — so copyViaShortcut's Clipboard-API write is
  // recognized as the same copy and dropped when the keydown ALSO reaches
  // the page (engine-dependent ordering).
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onCopy = (e: ClipboardEvent) => {
      // Same gate as copyViaShortcut: only a VISIBLE terminal selection claims
      // the event. Without it a copy from the find bar's input (inside this
      // container) or the desktop Edit ▸ Copy role with nothing highlighted
      // would be answered with a stale snapshot.
      if (!termRef.current?.hasSelection()) return;
      const sel = selectedTextForCopy();
      if (!sel) return;
      // Record this copy EVEN when an element-level handler already
      // answered it (defaultPrevented): the event is one half of a duplicate
      // pair (native menu selector + keydown for the same Cmd+C), and the
      // Clipboard-API keydown path must be suppressed for this same
      // keystroke whichever side ran first.
      markCopySeen(sel);
      if (e.defaultPrevented || !e.clipboardData) return;
      e.preventDefault();
      e.clipboardData.setData("text/plain", sel);
    };
    el.addEventListener("copy", onCopy);
    return () => el.removeEventListener("copy", onCopy);
  }, [selectedTextForCopy]);

  // Container-level `paste` listener, CAPTURE phase — the single paste path.
  //
  // A browser's Cmd/Ctrl+V default action is to dispatch a `paste`
  // ClipboardEvent on the focused editable element, which for a terminal is
  // ghostty-web's hidden textarea, and ghostty-web listens for that event
  // itself and pastes the clipboard. So the event
  // arrives whether or not the keydown handler touches the clipboard — which
  // is exactly why the keydown handler (below) must not read it: the read and
  // ghostty-web's listener each pasted the same text, so one Cmd+V pasted twice.
  //
  // Owning the event here (capture phase, so it runs BEFORE ghostty-web's own
  // listener, and stopPropagation keeps that listener from ever
  // running) means this function is the only thing that turns a paste into
  // terminal input. It also absorbs the desktop shell's second delivery route
  // for a single physical paste — the native Edit ▸ Paste role plus the
  // keydown default action, the same dual route the copy path has to dedupe —
  // so a same-text paste inside a short window is dropped instead of reaching
  // the pty twice.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let lastText = "";
    let lastAt = 0;
    const onPaste = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData("text/plain") ?? "";
      if (!text) return;
      // Only pastes aimed at the TERMINAL. The container also holds the find
      // bar's own search input, and a capture-phase listener on the panel root
      // runs for that too — without this guard a Cmd+V into the find field
      // would land in the shell.
      const term = termRef.current;
      if (!term?.element || !(e.target instanceof Node) || !term.element.contains(e.target)) return;
      // Consume in both branches: ghostty-web's own paste listener must never see
      // the event, whether we paste it or drop it as a duplicate.
      e.preventDefault();
      e.stopPropagation();
      const now = performance.now();
      if (text === lastText && now - lastAt < DUPLICATE_PASTE_WINDOW_MS) return;
      lastText = text;
      lastAt = now;
      // term.paste (not a raw socket write) so bracketed-paste mode is
      // honored for multiline payloads; it flows through onData to the pty.
      // Pasting is input: it ends the selection like a typed key does.
      if (term.hasSelection()) term.clearSelection();
      term.paste(text);
    };
    el.addEventListener("paste", onPaste, true);
    return () => el.removeEventListener("paste", onPaste, true);
  }, []);

  // Keep host in this lifecycle's dependencies. HomeApp gates startup on a
  // successful project-metadata snapshot, while a deliberate host identity
  // change must still rebuild history and the socket with the new destination.
  useEffect(() => {
    const el = containerRef.current;
    const termHost = hostRef.current;
    if (!el || !termHost) return;

    // Each panel loads its own libghostty-vt instance (ghosttyRuntime.ts says
    // why it is not shared); everything below runs after it resolves.
    // `cancelled` covers an unmount (or a project/host/id change) that lands
    // while the load is in flight.
    let cancelled = false;
    let cleanup: (() => void) | null = null;
    const setup = (ghostty: Ghostty): (() => void) => {
    const savedBuffer = loadTerminalBuffer(id);
    const term = new Terminal(
      buildTerminalOptions({
        scrollbackLines: scrollbackLinesRef.current,
        fontFamily,
        fontSize,
        savedBuffer,
        ghostty,
      }),
    );
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(termHost);
    // Everything here throws on a ghostty-web internals mismatch; an opened terminal
    // must not outlive a failed setup (render loop, listeners, wasm terminal).
    let detachMouse: () => void;
    try {
      // ghostty-web copies every mouse selection to the clipboard itself, registers
      // URL providers that window.open on ctrl/cmd-click, and reports the mouse itself.
      // ocode owns all three (deduped copy, openExternalURL, terminalMouse.ts), so the
      // built-ins are removed.
      disableBuiltinSelectionCopy(term);
      clearBuiltinLinkProviders(term);
      disableBuiltinMouseReporting(term);
      detachMouse = attachMouseReporting(term, termHost);
    } catch (err) {
      term.dispose();
      throw err;
    }
    const detachImeHygiene = term.textarea ? attachImeHygiene(term.textarea) : null;
    // Links: registerUrlLinkProvider handles http(s) URLs (e.g. Vite's localhost
    // link) on any plain left click (ghostty-web's own only fire on ctrl/cmd+click);
    // registerFileLinkProvider handles file paths (src/foo.ts:12, .ocode/uploads)
    // and dispatches ocode:open-file to open the editor. A drag that moved is a
    // selection, not a click (dragMovedRef). URLs route through openExternalURL
    // so the desktop shell opens them in the OS browser instead of losing the
    // webview to a navigation.
    const urlLinkProvider = registerUrlLinkProvider(
      term,
      (uri) => {
        if (/^https?:\/\//.test(uri)) openExternalURL(uri);
      },
      () => dragMovedRef.current,
    );
    const fileLinkProvider = registerFileLinkProvider(term, projectPath, () => dragMovedRef.current);
    const search = createTerminalSearch(term, (row, c0, c1) => setSelectionAbsolute(term, c0, row, c1, row));
    const searchDisp = search.onDidChangeResults((e) => {
      setFindResult({ count: e.resultCount, index: e.resultIndex });
    });
    termRef.current = term;
    registerTerminal(id, term);
    fitRef.current = fit;
    searchRef.current = search;

    // Custom key handler: Shift+Enter disambiguation, clipboard shortcuts
    // (copyViaShortcut/pasteFromClipboard below), Ctrl/Cmd+F for find,
    // Esc to close find, F3/Ctrl+G to navigate matches.
    // Return convention (INVERSE of xterm.js): true = handled here, skip
    // ghostty-web and cancel the browser default; false = let the terminal
    // process the key.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type !== "keydown") return false;
      // Cmd/Ctrl+C: copy when there is a selection (handled, so the keydown is
      // never converted to \x03). With no selection fall through so Ctrl+C
      // still sends SIGINT as usual.
      if ((ev.metaKey || ev.ctrlKey) && !ev.altKey && ev.key.toLowerCase() === "c") {
        if (copyViaShortcut()) {
          // Returning true also cancels the browser copy, so it cannot
          // overwrite our canvas selection with an empty one.
          return true;
        }
        // Ctrl+C with nothing highlighted is SIGINT: pty input (see the
        // fall-through rule at the end of this handler).
        if (!ev.metaKey) selectionSnapshotRef.current = "";
        return false;
      }
      // Cmd/Ctrl+V (and Ctrl+Shift+V): return false WITHOUT preventDefault.
      // ghostty-web already skips (ctrl|meta)+KeyV before encoding the key, so
      // no literal \x16 is sent; the browser's default action (dispatching the
      // `paste` event the container listener above owns) is what pastes.
      // Returning true would preventDefault the keydown and kill that event;
      // reading the clipboard here too is the double paste.
      if ((ev.metaKey || ev.ctrlKey) && !ev.altKey && ev.key.toLowerCase() === "v") {
        // The paste is pty input: drop the snapshot as any typed key would.
        selectionSnapshotRef.current = "";
        return false;
      }
      if (ev.key === "Enter" && ev.shiftKey) {
        selectionSnapshotRef.current = "";
        const sock = socketRef.current;
        if (sock && sock.readyState === WebSocket.OPEN) sock.send("\x1b[13;2u");
        // Handled: ghostty-web must not also encode this Enter as "\r", or the
        // pty would see Shift+Enter immediately followed by Enter.
        return true;
      }
      if ((ev.ctrlKey || ev.metaKey) && !ev.altKey && ev.key.toLowerCase() === "f") {
        const sel = term.getSelection();
        if (sel && !findOpenRef.current) setFindQuery(sel);
        setFindOpen(true);
        return true;
      }
      if (ev.key === "Escape" && findOpenRef.current) {
        setFindOpen(false);
        search.clear();
        setFindResult({ count: 0, index: -1 });
        return true;
      }
      const q = findQueryRef.current;
      if (q && q.trim()) {
        if (ev.key === "F3" || ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "g")) {
          if (ev.shiftKey) search.findPrevious(q);
          else search.findNext(q);
          return true;
        }
      }
      // A key the terminal will turn into pty input ends the selection the user
      // made; drop the snapshot with it so a later Copy never resurrects text
      // typed over. Bare modifiers and Cmd-combos (left to the browser on mac)
      // produce no input. ghostty-web keeps its selection across input (xterm
      // cleared it), so clear it here too: otherwise a Select All leaves every
      // later Ctrl+C a copy instead of SIGINT until the user clicks.
      if (!ev.metaKey && !MODIFIER_KEYS.has(ev.key)) {
        selectionSnapshotRef.current = "";
        if (term.hasSelection()) term.clearSelection();
      }
      return false;
    });

    // Attention signals: the BEL control char and the common notification OSC
    // sequences (9 = iTerm/most, 777/7777 = urxvt/others, 99 = kitty/notifications).
    // When the terminal is backgrounded we raise a "unread activity" badge and
    // (if the user enabled sound) play an alert. readyRef/activeRef keep the
    // handler stable across re-renders without re-subscribing; they also prevent
    // alerting for a BEL baked into the restored scrollback, or for a bell fired
    // while the user is already looking at this terminal.
    const onAttention = () => {
      if (!readyRef.current || activeRef.current) return;
      markAlerted(projectPath, id, host);
      playAlertSound();
    };

    // Copy-on-selection debounce: each onSelectionChange restarts the timer and
    // the copy fires once the selection has settled (i.e. at release). A cleared
    // selection (plain click, typing, buffer switch) must never touch the
    // clipboard and drops the snapshot. Only a pointer-driven selection is
    // copied here (userSelectingRef): the same event also fires for selectAll
    // and programmatic selections, and the rows under a selection may already
    // hold rewritten content.
    const selectionDisp = term.onSelectionChange(() => {
      if (copyDebounceRef.current !== null) {
        clearTimeout(copyDebounceRef.current);
        copyDebounceRef.current = null;
      }
      // A cleared selection leaves the snapshot alone: the terminal can clear
      // on scrollback trim, mouse-report input and buffer switch, none of which
      // is the user deselecting (that is the mouse-down / keydown paths).
      if (!term.hasSelection()) return;
      if (!userSelectingRef.current) return;
      copyDebounceRef.current = setTimeout(() => {
        copyDebounceRef.current = null;
        copySelectionNow();
      }, 150);
    });

    // OSC 0/2 window title from the running program (claude code, the ocode
    // TUI, shells with a title-setting prompt) becomes the tab name unless
    // the user renamed the tab. Titles replayed from restored scrollback are
    // fine to apply: they are the program's last known title.
    const applyProgramTitle = (title: string) => setOscTitle(projectPath, id, title, host);

    // OSC 52 clipboard write. Claude Code (mouse-tracking mode), tmux and vim
    // handle a drag-selection themselves and "copy" it by emitting
    // ESC ] 52 ; <targets> ; <base64> BEL — the only copy path that works
    // through ssh. ghostty-web ignores it, so the stream scanner (below)
    // hands it to handleOsc52. Write-only: a "?" query
    // would let remote programs read the local clipboard, so it is refused.
    // Replayed scrollback (readyRef false) must not overwrite the clipboard.
    // Hijack guard: a program that merely prints (`cat hostile`, a remote host)
    // has no user gesture behind it, so the write is honoured only within
    // OSC52_INPUT_WINDOW_MS of the user's last press, release or key in this
    // terminal (the drag-select release or keypress that triggered the copy),
    // and oversized payloads are dropped before they are decoded on the UI thread.
    let lastUserInputAt = Number.NEGATIVE_INFINITY;
    let pressInTerminal = false;
    const markUserInput = () => {
      lastUserInputAt = performance.now();
    };
    const onInputPointerDown = () => {
      pressInTerminal = true;
      markUserInput();
    };
    // The release can land outside the container (drag past its edge), so it
    // is watched on window, but only counts for a press that began here.
    const onInputPointerUp = () => {
      if (!pressInTerminal) return;
      pressInTerminal = false;
      markUserInput();
    };
    // A cancelled press (touch scroll, drag interruption) or a window blur never
    // reaches pointerup, so it must not leave the flag set for a later release.
    const onInputPressCancel = () => {
      pressInTerminal = false;
    };
    el.addEventListener("pointerdown", onInputPointerDown, true);
    el.addEventListener("keydown", markUserInput, true);
    window.addEventListener("pointerup", onInputPointerUp, true);
    window.addEventListener("pointercancel", onInputPressCancel, true);
    window.addEventListener("blur", onInputPressCancel);
    // `data` is "targets;base64", exactly the payload the stream scanner yields.
    const handleOsc52 = (data: string) => {
      if (!readyRef.current) return;
      const sep = data.indexOf(";");
      if (sep < 0) return;
      const payload = data.slice(sep + 1);
      if (payload === "?" || payload === "") return;
      if (payload.length > OSC52_MAX_PAYLOAD_CHARS) {
        console.warn("Terminal OSC 52 payload exceeds size cap; clipboard write refused");
        return;
      }
      if (performance.now() - lastUserInputAt > OSC52_INPUT_WINDOW_MS) {
        console.warn("Terminal OSC 52 write without recent user input refused");
        return;
      }
      let text: string;
      try {
        const bin = atob(payload);
        text = new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)));
      } catch (err) {
        console.warn("Terminal OSC 52 payload was not valid base64:", err);
        return;
      }
      void writeClipboardText(text);
    };

    // Attention, title and clipboard signals come from OUR scan of the pty byte
    // stream, not from ghostty-web's own events: term.onBell fires when a
    // written chunk contains BEL ANYWHERE, including the BEL that terminates an
    // OSC title string (so every title change would ring the bell);
    // term.onTitleChange sees OSC 0/1/2 only inside a single chunk, so a title
    // split across two frames is lost; and there is no OSC handler API at all
    // (xterm's parser.registerOscHandler). The scanner is stateful across
    // chunks, so split sequences are handled, and it runs in the hidden
    // background tab too because it is fed from the socket, not from rendering.
    const scanner = createTerminalStreamScanner({
      onOsc: (code, payload) => {
        if (code === 0 || code === 2) applyProgramTitle(payload);
        else if (code === 9 || code === 777 || code === 99) onAttention();
        else if (code === 52) handleOsc52(payload);
      },
      onBell: onAttention,
    });
    scannerRef.current = scanner;

    // Parsing is synchronous, so the snapshot is marked dirty by hand after
    // every write instead of from a "write parsed" event (ghostty-web has none).
    // Never acknowledge queued bytes before they have been written.
    const snapshot = createTerminalSnapshot(() => {
      saveTerminalBuffer(id, serializeTerminalBuffer(term, TERMINAL_THEME_DEFAULTS), term.cols, term.rows);
    });
    snapshotRef.current = snapshot;
    // EVERY pty-originated write goes through here so the scanner sees it.
    const writeOutput = (text: string) => {
      search.invalidate();
      scanner.scan(text);
      term.write(text);
      snapshot.markDirty();
    };
    // Banners ocode writes itself: shown on screen, never scanned.
    const writeBanner = (text: string) => {
      search.invalidate();
      term.write(text);
      snapshot.markDirty();
    };
    // A full reset is RIS (ESC c), parsed in place, never term.reset():
    // ghostty-web's reset() frees the wasm terminal and recreates it, which drops the
    // selection manager and link cache state out from under the panel. RIS clears
    // screen, scrollback and modes the way xterm's reset() did.
    const resetTerminal = () => {
      search.invalidate();
      term.write("\x1bc");
      scanner.reset();
      snapshot.markDirty();
    };
    resetTerminalRef.current = resetTerminal;
    const resizeDisp = term.onResize(() => {
      // A resize reflows the buffer, so the find bar's cached row text is stale too.
      search.invalidate();
      snapshot.markDirty();
    });
    const onSnapshotPointerDown = (event: PointerEvent) => {
      if (event.button === 0) snapshot.beginSelection();
    };
    // Capture phase: ghostty-web's wheel handler on the terminal host runs in
    // capture and always stopPropagation()s, so a bubble-phase listener on this
    // ancestor would never fire.
    const snapshotWheelOptions = { passive: true, capture: true } as const;
    el.addEventListener("wheel", snapshot.input, snapshotWheelOptions);
    // A wheel gesture over the terminal must never scroll the app chrome.
    // ghostty-web preventDefaults and consumes every wheel over the terminal
    // host itself (scrollback, arrow keys on the alt screen, or a mouse report
    // via terminalMouse.ts), so this guard now only sees wheels over the
    // panel's own chrome (find bar, overlays) and the container's padding. In
    // the desktop shell (WKWebView) even the non-scrollable document
    // rubber-bands, so those leftover gestures would move the whole app window.
    // Swallow them here, while still letting this container's own overflow
    // fallback scroll (a terminal taller than its box, e.g. between a
    // font-size change and the next fit). Bubble phase and non-passive.
    const onWheelGuard = (event: WheelEvent) => {
      if (event.defaultPrevented) return;
      const maxScrollTop = el.scrollHeight - el.clientHeight;
      const canScrollBox =
        event.deltaY < 0 ? el.scrollTop > 0 : event.deltaY > 0 ? el.scrollTop < maxScrollTop : false;
      if (canScrollBox) return;
      event.preventDefault();
    };
    el.addEventListener("wheel", onWheelGuard, { passive: false });
    // ghostty-web has no touch scrolling, so a finger drag on a phone or
    // tablet (mobile browser pointed at a shared desktop server) would do
    // nothing. Synthetic wheel events cannot stand in for it. Drive it through
    // the API instead — whole cell rows of drag become `scrollLines`, or, when
    // a TUI owns the mouse (mouse tracking on), one wheel report per row sent
    // to the app. The container is `touch-none`, so the browser never claims
    // the pan and these listeners can stay passive.
    let touchY: number | null = null;
    let touchRemainder = 0;
    const onTouchStart = (event: TouchEvent) => {
      touchY = event.touches.length === 1 ? event.touches[0].clientY : null;
      touchRemainder = 0;
    };
    const onTouchMove = (event: TouchEvent) => {
      const canvas = termHost.querySelector("canvas");
      if (touchY === null || event.touches.length !== 1 || !canvas) return;
      const touch = event.touches[0];
      const rect = canvas.getBoundingClientRect();
      const cellHeight = rect.height / term.rows;
      touchRemainder += touchY - touch.clientY;
      touchY = touch.clientY;
      // Drag down = finger pulls older content into view = scroll up (negative).
      const rows = Math.trunc(touchRemainder / cellHeight);
      if (rows === 0) return;
      touchRemainder -= rows * cellHeight;
      if (!term.hasMouseTracking()) {
        term.scrollLines(rows);
        return;
      }
      sendWheelReport(term, rows, touch.clientX, touch.clientY);
    };
    const onTouchEnd = () => {
      touchY = null;
      touchRemainder = 0;
    };
    el.addEventListener("touchstart", onTouchStart, { passive: true });
    el.addEventListener("touchmove", onTouchMove, { passive: true });
    el.addEventListener("touchend", onTouchEnd, { passive: true });
    el.addEventListener("touchcancel", onTouchEnd, { passive: true });
    el.addEventListener("keydown", snapshot.input, true);
    el.addEventListener("pointerdown", onSnapshotPointerDown, true);
    window.addEventListener("pointerup", snapshot.endSelection, true);
    window.addEventListener("pointercancel", snapshot.endSelection, true);
    window.addEventListener("blur", snapshot.endSelection);
    const doSave = snapshot.flush;
    const saveInterval = setInterval(snapshot.schedule, 30000);
    const onPageHide = () => doSave();
    document.addEventListener("visibilitychange", onPageHide);
    window.addEventListener("pagehide", onPageHide);

    let sock: WebSocket | null = null;
    let chunkRafId = 0;
    const pendingChunks: string[] = [];
    let pendingBytes = 0;
    let restoreCancelled = false;
    // Set by the unmount cleanup. The cleanup closes the socket, which fires
    // onclose asynchronously — after the cleanup has already cleared the
    // pending reconnect timer. Without this guard that late onclose schedules
    // a fresh reconnect from an unmounted panel, and the ghost socket
    // reattaches to (or, if the shell was just killed, respawns) the terminal
    // under the closed tab's id.
    let disposed = false;
    const restoreController = new AbortController();
    let serverHistoryRestored = false;
    let serverHistoryPartial = false;
    const terminalDecoder = new TextDecoder();

    // Holds the panel until a remote host's retry window closes, then runs
    // `next`. The banner prints once: a wake or restore re-entering while the
    // timer is armed just moves it to the window's end.
    //
    // Re-check at most every REMOTE_HOST_RECHECK_MS instead of sleeping the
    // whole window. The gate can clear the window EARLY — the status probe
    // (an in-memory registry read) sees the host recover — and a single
    // multi-minute timeout would keep this panel dead long after the host is
    // back. The check reads only in-memory gate state, so it costs no request.
    const waitForRemoteHost = (waitMs: number, next: () => void, silent = false) => {
      if (!silent && reconnectTimerRef.current === null) {
        writeBanner(
          "\r\n\x1b[33m[remote host unreachable — next attempt in " +
            Math.ceil(waitMs / 1000) +
            "s]\x1b[0m\r\n",
        );
      }
      if (reconnectTimerRef.current !== null) clearTimeout(reconnectTimerRef.current);
      reconnectingRef.current = true;
      reconnectTimerRef.current = window.setTimeout(() => {
        reconnectTimerRef.current = null;
        const remaining = host ? remoteHostRetryInMs(host) : 0;
        if (remaining > 0) {
          waitForRemoteHost(remaining, next, true);
          return;
        }
        next();
      }, Math.min(waitMs, REMOTE_HOST_RECHECK_MS));
    };

    const connectSocket = (historyOffset?: number) => {
      // A remote host inside its retry window is not dialled: each attempt
      // holds a connection for the whole ssh timeout, and that starves the
      // browser's connections for everything else. Wait out the window instead.
      const waitMs = host ? remoteHostRetryInMs(host) : 0;
      if (waitMs > 0) {
        waitForRemoteHost(waitMs, () => connectSocket(historyOffset));
        return;
      }
      const { url, protocols } = buildTerminalWsConnection({
        token: authToken(),
        projectPath,
        host,
        terminalId: id,
        isRemote: isRemoteSession(),
        historyOffset,
      });
      const nextSocket = new WebSocket(url, protocols);
      nextSocket.binaryType = "arraybuffer";
      // True when this socket carries a REST history cursor: the client just
      // replayed scrollback from disk, and any mode-setting sequence inside
      // that replay is a recording of the past. In particular a TUI that
      // enabled mouse tracking before the desktop app quit (remote workspaces
      // keep the shell alive on the remote server) left a DECSET in the log
      // that the replay re-applies; the TUI itself is gone, so the mode must be
      // cleared even though the reattach is `resumed:true`.
      const isHistoryAttach = historyOffset !== undefined;
      // A supersede race (the reconnect timer and an onWake/wake-triggered
      // connect both firing, or a late history-restore fallback) must never
      // leave two live sockets for one terminal id. Two sockets attach to the
      // same server session; each new attach closes the previous one, whose
      // onclose then reconnects again — an endless ~1s ping-pong that also
      // respawns the shell after a tab close (each reconnect re-creates the
      // session if the kill already landed). Close the socket this panel was
      // still holding before opening its replacement; the stale socket's
      // onclose is ignored because socketRef no longer points at it (the
      // handler guards below compare against socketRef.current).
      const previous = socketRef.current;
      sock = nextSocket;
      socketRef.current = nextSocket;
      if (
        previous &&
        previous !== nextSocket &&
        (previous.readyState === WebSocket.OPEN || previous.readyState === WebSocket.CONNECTING)
      ) {
        try {
          previous.close();
        } catch {
          // Already closing/closed — nothing to clean up.
        }
      }
      // Opening a socket supersedes any pending reconnect: a history-restore
      // attach (connectSocket(result.snapshotEnd)) or an onWake can race the
      // 1s reconnect timer and otherwise open a second socket. Cancel it here
      // rather than relying on onopen, which may run after the timer fired.
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      reconnectingRef.current = false;
      // Reset only the handshake gate here — NOT reconnectAttemptRef. This
      // function also runs from the reconnect timer, so clearing the attempt
      // counter on every call would pin the backoff at its 1s floor and hammer
      // an unreachable remote. onopen resets it once a socket actually opens.
      attachedRef.current = false;
      setShellReady(false);

      connectSocketRef.current = () => connectSocket();
      nextSocket.onopen = () => {
        // A superseded socket can still fire onopen (it was CONNECTING when
        // replaced). Ignore it so the supersede can't steal the fit/resize or
        // reset the live socket's backoff state.
        if (disposed || socketRef.current !== nextSocket) return;
        if (host) noteRemoteHostReachable(host);
        readyRef.current = true;
        fitAndResize.current();
        // A fresh connection succeeded — cancel any pending reconnect
        // timer and reset attempt counter so backoff starts from scratch
        // on the next unexpected drop.
        if (reconnectTimerRef.current !== null) {
          clearTimeout(reconnectTimerRef.current);
          reconnectTimerRef.current = null;
        }
        reconnectAttemptRef.current = 0;
        // A fresh socket owns the terminal again; drop the takeover banner.
        if (takenOverRef.current) {
          takeoverRef.current = false;
          takenOverRef.current = false;
          setTakenOver(false);
        }
      };
      // Chunk large live writes to prevent memory spikes from one-shot decode+write.
      // This cap only applies after the complete server restore has finished;
      // dropped live bytes remain explicitly marked and are still persisted by
      // the server history log.
      const CHUNK_THRESHOLD = 64 * 1024;
      const CHUNK_SIZE = 16 * 1024;
      const PENDING_CHUNK_CAP = 2 * 1024 * 1024;
      let outputDropped = false;
      const flushChunks = () => {
        chunkRafId = 0;
        for (let i = 0; i < 4 && pendingChunks.length > 0; i++) {
          const chunk = pendingChunks.shift()!;
          pendingBytes -= chunk.length;
          writeOutput(chunk);
        }
        if (pendingChunks.length > 0) {
          chunkRafId = requestAnimationFrame(flushChunks);
        } else {
          outputDropped = false;
        }
      };
      // Drain every queued render chunk synchronously, then report whether
      // anything is still in flight. onclose uses this so the final
      // localStorage save observes the complete last output — not whatever
      // happened to be rendered when the socket closed.
      const flushChunksSync = (): boolean => {
        if (chunkRafId) cancelAnimationFrame(chunkRafId);
        chunkRafId = 0;
        while (pendingChunks.length > 0) {
          const chunk = pendingChunks.shift()!;
          pendingBytes -= chunk.length;
          writeOutput(chunk);
        }
        outputDropped = false;
        return pendingBytes === 0 && pendingChunks.length === 0;
      };
      nextSocket.onmessage = (ev) => {
        // Ignore frames from a socket that has been superseded; its buffered
        // output was replaced by the new socket's own replay.
        if (socketRef.current !== nextSocket) return;
        if (typeof ev.data === "string") {
          let msg: { type?: string; resumed?: boolean; reason?: string };
          try {
            msg = JSON.parse(ev.data);
          } catch (err) {
            console.error("terminal: unparseable control frame", ev.data, err);
            return;
          }
          if (msg.type === "detached") {
            // Another client attached this terminal id and took the single
            // attachment slot. The server closes this socket immediately after,
            // so without recording the reason the close is indistinguishable
            // from a shell exit — and both auto-reconnect paths (backoff timer,
            // onWake) would re-attach, taking the slot back and starting a
            // two-client ping-pong that also respawns the shell after a close.
            // Arm the takeover state; onclose turns it into the UI below.
            takeoverRef.current = true;
            return;
          }
          if (msg.type === "attach") {
            // The handshake is done: from here term.onData may drive the pty.
            attachedRef.current = true;
            setShellReady(true);
            if (!msg.resumed || isHistoryAttach) {
              // Stale mouse modes survive two ways:
              //   - a fresh shell (previous session gone: remote SSH timeout/
              //     crash), where the TUI died without sending DECRST;
              //   - a history-cursor reattach (desktop app restart with a
              //     remote workspace), where the replayed scrollback re-applied
              //     a dead TUI's DECSET.
              // Either way the emulator would keep encoding mouse moves into escape
              // sequences the bare shell echoes as garbage. A plain resumed
              // attach (transient blip, in-memory replay, app still alive) is
              // left untouched so an app that still owns the mouse keeps it.
              writeBanner(TERMINAL_MOUSE_RESET);
            }
            if (msg.resumed && !serverHistoryRestored) {
              // A cursor attach replays only bytes after the REST snapshot, so
              // the restored terminal buffer must stay intact. The old no-cursor
              // attach path still clears localStorage fallback before capped
              // replay.
              resetTerminal();
            }
          }
          return;
        }
        const decoded = terminalDecoder.decode(new Uint8Array(ev.data as ArrayBuffer), { stream: true });
        if (decoded.length < CHUNK_THRESHOLD && pendingChunks.length === 0) {
          writeOutput(decoded);
          return;
        }
        for (let i = 0; i < decoded.length; i += CHUNK_SIZE) {
          const chunk = decoded.slice(i, i + CHUNK_SIZE);
          if (pendingBytes + chunk.length > PENDING_CHUNK_CAP) {
            if (!outputDropped) {
              outputDropped = true;
              writeBanner("\r\n\x1b[33m[terminal output truncated while rendering]\x1b[0m\r\n");
            }
            break;
          }
          pendingChunks.push(chunk);
          pendingBytes += chunk.length;
        }
        if (chunkRafId === 0) chunkRafId = requestAnimationFrame(flushChunks);
      };
      nextSocket.onerror = () => {
        if (disposed || socketRef.current !== nextSocket) return;
        console.error("terminal: websocket error on", url);
        writeBanner("\r\n\x1b[31m[terminal connection error]\x1b[0m\r\n");
      };
      nextSocket.onclose = (ev) => {
        // The panel unmounted (tab/project closed): never reconnect. The
        // cleanup already saved the buffer and cleared its timer; a reconnect
        // here would outlive the component and resurrect the shell.
        // A superseded socket (socketRef moved on) must also stay dead: it was
        // replaced deliberately, and reconnecting from it would start the
        // two-socket ping-pong the supersede path exists to stop.
        if (disposed || socketRef.current !== nextSocket) return;
        setShellReady(false);
        const remainder = terminalDecoder.decode();
        // Flush every queued render chunk BEFORE the ended banner and the
        // final save: WebSocket frames are decoded into pendingChunks and
        // painted via requestAnimationFrame, so without this the save would
        // capture only whatever happened to be rendered when the socket
        // closed. Writes are parsed synchronously, so once the flush and the
        // decoder remainder are written the saved buffer holds the complete
        // last output.
        flushChunksSync();
        if (remainder) writeOutput(remainder);
        if (takeoverRef.current) {
          // Another client owns this terminal now. Show the takeover state
          // and reconnect NOTHING: both auto-reconnect paths would take the
          // slot back, and the two clients would ping-pong. The user takes it
          // back deliberately via the button.
          // takeoverRef STAYS set for the lifetime of the takeover state —
          // it is the guard onWake reads. It is cleared only by the Take over
          // button or by a successful reopen, so clearing it here would let
          // the next wake steal the terminal straight back.
          takenOverRef.current = true;
          reconnectingRef.current = false;
          setTakenOver(true);
          writeBanner(
            "\r\n\x1b[33m[another client took over this terminal — reconnecting is paused]\x1b[0m\r\n",
          );
          doSave();
          return;
        }
        if (manualCloseRef.current || (ev.wasClean && ev.code === 1000)) {
          // Intentional close (user tab close, server kill) or natural
          // shell exit: session is definitively over — show ended banner,
          // no reconnect. A clean 1000 close with no manual flag means
          // the pty read loop ended (the shell itself exited); reconnecting
          // here would spawn a fresh shell under a different id.
          writeBanner("\r\n\x1b[33m[terminal session ended]\x1b[0m\r\n");
          // The server persists the full pty transcript to history before
          // it closes this socket (exit teardown syncs/closes the log
          // first), so the rendered buffer on screen IS the complete last
          // output. Save it synchronously: desktop quit tears down the
          // webview right after the server drains the shells, so the
          // deferred idle save may never run — this is what keeps the last
          // output in the tab after restart.
          doSave();
        } else {
          // Unexpected close — attempt to reconnect with exponential
          // backoff so a transient network blip (common with remote
          // SSH/WSL tunnels) doesn't cost the user their terminal.
          // This covers abnormal closures (code 1006, network drops,
          // server crashes) and server-detected dead connections.
          const attempt = reconnectAttemptRef.current;
          const backoff = Math.min(1000 * Math.pow(2, attempt), 32000);
          reconnectAttemptRef.current = attempt + 1;
          reconnectingRef.current = true;
          writeBanner(
            "\r\n\x1b[33m[terminal connection lost — reconnecting in " +
              (backoff / 1000).toFixed(0) +
              "s…]\x1b[0m\r\n",
          );
          console.error(
            `terminal: websocket closed unexpectedly (attempt ${attempt + 1}, reconnecting in ${(backoff / 1000).toFixed(0)}s)`,
            ev.code,
            ev.reason,
          );
          reconnectTimerRef.current = window.setTimeout(() => {
            reconnectTimerRef.current = null;
            connectSocket();
          }, backoff);
        }
      };
    };

    // The live socket is only opened after the REST restore settles, so a
    // restore that never settles would leave the terminal blank forever. Bound
    // it; each page re-arms the timer (see armRestoreTimeout below).
    let restoreSettled = false;
    let restoreTimer: number | null = null;
    const armRestoreTimeout = () => {
      if (restoreTimer !== null) clearTimeout(restoreTimer);
      restoreTimer = window.setTimeout(() => {
        restoreTimer = null;
        if (restoreSettled || restoreCancelled) return;
        restoreSettled = true;
        // Cancel the stalled fetch and attach without a history cursor — the
        // same fallback the restore-failure path uses.
        restoreController.abort();
        if (serverHistoryPartial) resetTerminal();
        writeBanner(
          "\r\n\x1b[33m[terminal history restore timed out; live terminal attached without restore]\x1b[0m\r\n",
        );
        connectSocket();
      }, TERMINAL_HISTORY_RESTORE_TIMEOUT_MS);
    };
    // The restore starts only once the host can be asked. A remote host inside
    // its retry window is not asked for history (the request would hang for the
    // whole ssh timeout), so the mount waits out the window and restores then.
    const startRestore = () => {
      armRestoreTimeout();

      void restoreTerminalHistory({
        id,
        projectPath,
        host,
        decoder: terminalDecoder,
        maxBytes: TERMINAL_HISTORY_RESTORE_MAX_BYTES,
        signal: restoreController.signal,
        onText: (text) => {
          if (restoreCancelled || restoreController.signal.aborted) return;
          // Progress: the restore is alive, so give it another full window.
          armRestoreTimeout();
          serverHistoryPartial = true;
          // Parsing is synchronous, so a page is fully applied when this returns.
          writeOutput(text);
        },
      }).then((result) => {
        restoreSettled = true;
        if (restoreTimer !== null) {
          clearTimeout(restoreTimer);
          restoreTimer = null;
        }
        if (restoreCancelled) return;
        // The bounded restore timer may have already fired — it aborts the fetch
        // and attaches the live socket. If the fetch had still resolved, calling
        // connectSocket here would open a second socket for the same pty. Bail
        // the same way the .catch path does.
        if (restoreController.signal.aborted) return;
        if (result.kind === "missing") {
          // Only announce the fallback when there is actually a cached
          // buffer to fall back to; a brand-new terminal has nothing to restore.
          if (savedBuffer) {
            writeOutput(savedBuffer.text);
            writeBanner("\r\n\x1b[2m── local terminal cache fallback; server history unavailable ──\x1b[0m\r\n");
          }
          connectSocket();
          return;
        }
        serverHistoryRestored = true;
        connectSocket(result.snapshotEnd);
      }).catch((err: unknown) => {
        restoreSettled = true;
        if (restoreTimer !== null) {
          clearTimeout(restoreTimer);
          restoreTimer = null;
        }
        if (restoreCancelled || restoreController.signal.aborted) return;
        // Do not leave a partial server replay on screen before attaching
        // without a cursor. A resumed shell will send its capped replay, while
        // a fresh shell starts from a clean buffer; either way this avoids
        // overlapping partial output and live replay.
        if (serverHistoryPartial) resetTerminal();
        const message = err instanceof TerminalHistoryError ? err.message : "terminal history restore failed";
        console.error("terminal: history restore failed", err);
        writeBanner(`\r\n\x1b[31m[${message}; live terminal attached without restore]\x1b[0m\r\n`);
        connectSocket();
      });
    };

    const waitThenRestore = () => {
      const waitMs = host ? remoteHostRetryInMs(host) : 0;
      if (waitMs > 0) {
        waitForRemoteHost(waitMs, waitThenRestore);
        return;
      }
      startRestore();
    };
    waitThenRestore();

    // Everything the client sends is text; the backend treats a frame starting
    // with {"type":"resize" as a control message and anything else as raw
    // keystrokes.
    const dataSub = term.onData((data) => {
      // Wait for the attach handshake: mouse reports the terminal still holds from a
      // dead session must not reach the new pty before the fresh-shell reset.
      if (attachedRef.current && sock?.readyState === WebSocket.OPEN) sock.send(data);
    });

    // On wake (network back / tab visible), skip the exponential backoff and
    // reconnect now if the socket is not open. Without this, a laptop that
    // slept mid-backoff can wait up to 30s before the first retry.
    const offWake = onWake(() => {
      if (disposed) return;
      // A taken-over terminal stays taken until the user asks for it back;
      // reconnecting here would evict the other client on every wake.
      if (takeoverRef.current) return;
      if (sock && (sock.readyState === WebSocket.OPEN || sock.readyState === WebSocket.CONNECTING)) return;
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      reconnectAttemptRef.current = 0;
      connectSocket();
    });

    const observer = new ResizeObserver(() => fitAndResize.current());
    observer.observe(el);

    return () => {
      disposed = true;
      restoreCancelled = true;
      restoreController.abort();
      if (restoreTimer !== null) {
        clearTimeout(restoreTimer);
        restoreTimer = null;
      }
      doSave();
      clearInterval(saveInterval);
      snapshot.dispose();
      snapshotRef.current = null;
      resizeDisp.dispose();
      el.removeEventListener("wheel", snapshot.input, snapshotWheelOptions);
      el.removeEventListener("wheel", onWheelGuard);
      el.removeEventListener("touchstart", onTouchStart);
      el.removeEventListener("touchmove", onTouchMove);
      el.removeEventListener("touchend", onTouchEnd);
      el.removeEventListener("touchcancel", onTouchEnd);
      detachImeHygiene?.();
      el.removeEventListener("keydown", snapshot.input, true);
      el.removeEventListener("pointerdown", onSnapshotPointerDown, true);
      el.removeEventListener("pointerdown", onInputPointerDown, true);
      el.removeEventListener("keydown", markUserInput, true);
      window.removeEventListener("pointerup", onInputPointerUp, true);
      window.removeEventListener("pointercancel", onInputPressCancel, true);
      window.removeEventListener("blur", onInputPressCancel);
      window.removeEventListener("pointerup", snapshot.endSelection, true);
      window.removeEventListener("pointercancel", snapshot.endSelection, true);
      window.removeEventListener("blur", snapshot.endSelection);
      document.removeEventListener("visibilitychange", onPageHide);
      window.removeEventListener("pagehide", onPageHide);
      offWake();
      observer.disconnect();
      dataSub.dispose();
      selectionDisp.dispose();
      if (copyDebounceRef.current !== null) {
        clearTimeout(copyDebounceRef.current);
        copyDebounceRef.current = null;
      }
      searchDisp.dispose();
      search.dispose();
      detachMouse();
      scanner.reset();
      scannerRef.current = null;
      resetTerminalRef.current = null;
      // No-ops today (ghostty-web unregisters providers only in term.dispose()),
      // kept so a future version that returns real disposables is honoured.
      fileLinkProvider.dispose();
      urlLinkProvider.dispose();
      // Cancel any pending chunk writes.
      if (chunkRafId) cancelAnimationFrame(chunkRafId);
      pendingChunks.length = 0;
      // Stop any pending reconnection attempts — component is
      // unmounting so there's no audience for a reconnect.
      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      reconnectingRef.current = false;
      // Closing the socket only detaches the shell server-side: it survives
      // for the detach TTL so a reload/remount reattaches to it. Explicit tab
      // close kills it via DELETE /api/terminal/{id} in the terminal store.
      sock?.close();
      socketRef.current = null;
      term.dispose();
      unregisterTerminal(id);
      termRef.current = null;
      fitRef.current = null;
      searchRef.current = null;
    };
    };

    loadGhostty()
      .then((ghostty) => {
        if (cancelled) return;
        cleanup = setup(ghostty);
        // The active-tab effect ran before the terminal existed.
        if (activeRef.current) termRef.current?.focus();
      })
      .catch((err: unknown) => {
        console.error("terminal: failed to start the ghostty-web terminal", err);
        setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
      cleanup?.();
    };
    // Backend switches intentionally do NOT restart existing terminals:
    // the PTY is host-local and would be lost. New terminals after a switch
    // use the new apiWsPath; a full reload migrates all.
  }, [projectPath, host, id]);

  // Apply a changed scrollback setting to the open terminal. ghostty-web cannot resize the
  // scrollback of a live wasm terminal, but reset() rebuilds it from `options.scrollback`,
  // so: serialize the buffer, set the option, reset, replay (the same path as the mount
  // restore). The pty and socket are untouched. Lost by design: terminal modes (mouse
  // tracking, alternate screen, bracketed paste), the selection, the link cache, and a
  // replayed row that was cut off by a smaller limit. Output written during the rebuild
  // cannot interleave: reset, write and the option change are all synchronous.
  useEffect(() => {
    const term = termRef.current;
    if (!term || term.options.scrollback === scrollbackLines) return;
    const text = serializeTerminalBuffer(term, TERMINAL_THEME_DEFAULTS);
    term.options.scrollback = scrollbackLines;
    term.reset();
    term.write(text);
    term.clearSelection();
    searchRef.current?.invalidate();
    snapshotRef.current?.markDirty();
  }, [scrollbackLines]);

  // Apply font changes to the live terminal in place instead of tearing down
  // the session (which would kill the pty/websocket) whenever settings change.
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    term.options.fontFamily = fontFamily;
    term.options.fontSize = fontSize;
    fitAndResize.current();
  }, [fontFamily, fontSize]);

  // Re-fit when the tab comes back to the foreground: any resize that happened
  // while hidden was skipped because the container had no layout.
  // Also focus the terminal so the user can type immediately.
  useEffect(() => {
    if (active) {
      fitAndResize.current();
      termRef.current?.focus();
    }
  }, [active]);

  // Allow the tab bar to focus this shell on single left-click, even when
  // `active` does not change (already-active tab). The registry is keyed by
  // terminal id so UnifiedTabBar can call focus without threading refs through App.
  useEffect(() => {
    const doFocus = () => termRef.current?.focus();
    registerTerminalFocus(id, doFocus);
    return () => unregisterTerminalFocus(id);
  }, [id]);

  // Clamp menu to viewport (same strategy as Layout/ContextMenu)
  const clampedMenuPos = ctxMenu
    ? {
        x: Math.min(ctxMenu.x, typeof window !== "undefined" ? window.innerWidth - 220 : ctxMenu.x),
        y: Math.min(ctxMenu.y, typeof window !== "undefined" ? window.innerHeight - 360 : ctxMenu.y),
      }
    : null;

  const menuItems: Array<{
    label: string;
    icon: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
    separator?: boolean;
    destructive?: boolean;
  }> = ctxMenu
    ? [
        { label: "Copy", icon: <Copy className="h-4 w-4" />, onClick: handleCopy, disabled: !ctxMenu.hasSelection },
        { label: "Play selection", icon: <Volume2 className="h-4 w-4" />, onClick: handleSpeakSelection, disabled: !ctxMenu.hasSelection },
        { label: "Paste", icon: <ClipboardPaste className="h-4 w-4" />, onClick: handlePaste },
        { label: "Select All", icon: <CopyPlus className="h-4 w-4" />, onClick: handleSelectAll },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "Clear Terminal", icon: <Trash2 className="h-4 w-4" />, onClick: handleClear },
        { label: "Reset Terminal", icon: <RotateCcw className="h-4 w-4" />, onClick: handleReset },
        { label: "Scroll to Top", icon: <ArrowUpToLine className="h-4 w-4" />, onClick: handleScrollTop },
        { label: "Scroll to Bottom", icon: <ArrowDownToLine className="h-4 w-4" />, onClick: handleScrollBottom },
        { label: "Speak visible", icon: <Volume2 className="h-4 w-4" />, onClick: handleSpeakVisible },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "Find…", icon: <Search className="h-4 w-4" />, onClick: handleFind },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "New Terminal", icon: <Plus className="h-4 w-4" />, onClick: handleNewTerminal },
        { label: "Close Terminal", icon: <X className="h-4 w-4" />, onClick: handleCloseTerminal, destructive: true },
      ]
    : [];

  const voiceLabel =
    voice.state === "recording"
      ? "Stop recording"
      : voice.state === "transcribing"
        ? "Transcribing voice input"
        : "Start voice input";
  const voiceTitle = !voice.supported
    ? voiceUnavailableMessage()
    : voice.state === "idle" && !shellReady
      ? "Terminal is not connected"
      : voiceLabel;

  return (
    <>
    <div
      ref={containerRef}
      // This div is the fit parent of the terminal host, so it stays
      // padding-free: FitAddon subtracts the HOST's own CSS padding from the
      // host's border-box size, so the 8px ring lives on the host (`p-2`, same
      // color: --card equals the terminal background #18181b) and the fit is
      // exact. overflow-y-auto is the requested "vertical scroll: auto": a
      // scrollbar appears only when content genuinely overflows (font resize
      // before refit, tiny windows where even one row doesn't fit).
      // Horizontal overflow belongs to the terminal, hence overflow-x-hidden.
      className="relative h-full w-full bg-card overflow-y-auto overflow-x-hidden overscroll-contain touch-none"
      onContextMenu={handleContextMenu}
      // Capture phase: under mouse tracking terminalMouse.ts stops a left press on the
      // host, which would leave these refs stale and the link-click guard (dragMovedRef) stuck.
      onMouseDownCapture={(e) => {
        // The context menu is portaled to <body>, but React still bubbles its
        // events through this container: a press on a menu item is not a
        // gesture in the terminal.
        if (!isTerminalPress(e)) return;
        // A fresh left press starts a new selection or deselects; either way
        // the previous drag's text is no longer what the user sees selected.
        selectionSnapshotRef.current = "";
        dragStartedRef.current = true;
        userSelectingRef.current = true;
        dragMovedRef.current = false;
        dragStartXRef.current = e.clientX;
        dragStartYRef.current = e.clientY;
      }}
      onMouseMove={(e) => {
        if (!dragStartedRef.current) return;
        const dx = Math.abs(e.clientX - dragStartXRef.current);
        const dy = Math.abs(e.clientY - dragStartYRef.current);
        if (dx > 2 || dy > 2) dragMovedRef.current = true;
      }}
      onMouseUp={(e) => {
        if (!isTerminalPress(e)) return;
        dragStartedRef.current = false;
        // Copy-on-selection at release, inside the user-gesture handler (see
        // copySelectionNow above for the TUI-parity rationale). No-op on a
        // plain click without a selection, so the clipboard is never clobbered.
        copySelectionNow();
      }}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div ref={hostRef} className="terminal-host h-full w-full p-2" />
      {loadError && (
        <div
          role="alert"
          data-terminal-chrome=""
          className="absolute inset-x-0 top-2 z-10 mx-auto w-fit max-w-[90%] rounded-md border border-red-500/40 bg-card/95 px-3 py-1.5 text-xs text-red-300 shadow-lg"
        >
          Terminal failed to start: {loadError}
        </div>
      )}
      {takenOver && (
        <div
          role="status"
          data-terminal-chrome=""
          className="absolute inset-x-0 bottom-2 z-10 mx-auto flex w-fit max-w-[90%] items-center gap-3 rounded-md border border-amber-500/40 bg-card/95 px-3 py-1.5 text-xs text-amber-200 shadow-lg"
        >
          <span>Taken over by another client — reconnecting is paused.</span>
          <button
            type="button"
            className="rounded border border-amber-500/50 px-2 py-0.5 font-medium hover:bg-amber-500/10"
            onClick={() => {
              // Deliberate hand-back: clear the guard, then reconnect, which
              // evicts the other client's socket (single attachment slot).
              takeoverRef.current = false;
              takenOverRef.current = false;
              setTakenOver(false);
              connectSocketRef.current?.();
            }}
          >
            Take over
          </button>
        </div>
      )}
      {findOpen && (
        <TerminalFindBar
          query={findQuery}
          onQueryChange={handleFindQueryChange}
          resultCount={findResult.count}
          resultIndex={findResult.index}
          onNext={handleFindNext}
          onPrev={handleFindPrev}
          onClose={handleCloseFind}
        />
      )}
      {isDragging && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-md border-2 border-dashed border-blue-500 bg-blue-500/10">
          <span className="text-sm font-medium text-blue-300">Drop files here</span>
        </div>
      )}
      {ctxMenu &&
        clampedMenuPos &&
        createPortal(
          <div
            ref={ctxMenuRef}
            role="menu"
            className="fixed z-50 min-w-[200px] bg-popover border border-border rounded-md shadow-md py-1 animate-in fade-in-0 zoom-in-95"
            style={{ left: clampedMenuPos.x, top: clampedMenuPos.y }}
            onClick={(e) => e.stopPropagation()}
          >
            {menuItems.map((item, i) => {
              if (item.separator) return <div key={i} className="h-px bg-border my-1" />;
              return (
                <button
                  key={i}
                  role="menuitem"
                  disabled={!!item.disabled}
                  className={`w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left ${
                    item.destructive
                      ? "text-destructive hover:bg-destructive/10"
                      : "text-foreground hover:bg-accent hover:text-accent-foreground"
                  } ${item.disabled ? "opacity-50 pointer-events-none" : ""}`}
                  onClick={item.onClick}
                >
                  {item.icon && <span className="w-4 h-4 shrink-0 flex items-center justify-center">{item.icon}</span>}
                  {item.label}
                </button>
              );
            })}
          </div>,
          document.body,
        )}
      {/* data-terminal-chrome keeps a tap on the mic from reading as a terminal
          press (it would reset the selection snapshot and arm copy-on-select).
          mousedown is cancelled so focus stays in the terminal and the phone
          keyboard does not drop. aria-disabled, not disabled, when recording is
          unsupported: a native disabled button swallows the tap that explains why. */}
      <div data-terminal-chrome="" className="absolute bottom-3 right-3 z-10 flex items-center gap-2">
        {voice.state !== "idle" && (
          <span role="status" className="rounded-md border border-border bg-card/95 px-2 py-1 text-xs text-muted-foreground shadow-lg">
            {voice.state === "recording"
              ? "Recording… tap to stop"
              : voice.installing
                ? "Installing voice dependencies… this can take several minutes"
                : "Transcribing…"}
          </span>
        )}
        <button
          type="button"
          onMouseDown={(e) => e.preventDefault()}
          onClick={voice.toggle}
          disabled={voice.state === "transcribing" || (voice.state === "idle" && !shellReady)}
          aria-disabled={!voice.supported || undefined}
          aria-pressed={voice.state === "recording"}
          aria-label={voiceLabel}
          title={voiceTitle}
          className={`flex h-11 w-11 items-center justify-center rounded-full border shadow-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 ${
            voice.state === "recording"
              ? "border-transparent bg-destructive text-destructive-foreground hover:bg-destructive/90"
              : "border-border bg-card/95 text-foreground hover:bg-accent hover:text-accent-foreground"
          } ${voice.supported ? "" : "opacity-50"}`}
        >
          {voice.state === "transcribing" ? (
            <Loader2 aria-hidden="true" className="h-5 w-5 animate-spin" />
          ) : voice.state === "recording" ? (
            <Square aria-hidden="true" className="h-5 w-5" />
          ) : voice.supported ? (
            <Mic aria-hidden="true" className="h-5 w-5" />
          ) : (
            <MicOff aria-hidden="true" className="h-5 w-5" />
          )}
        </button>
      </div>
    </div>
    {/* Outside the container on purpose: React bubbles a portal's events up
        through this tree, so a click in the dialog would otherwise hit the
        container's mousedown/mouseup (selection reset, copy-on-select). */}
    <ConfirmDialog
      open={voice.installAsk !== null}
      title="Install voice dependencies?"
      description={`${voice.installAsk?.message ?? ""}. This can take several minutes.`}
      confirmLabel="Install"
      confirmVariant="default"
      onConfirm={async () => voice.answerInstall(true)}
      onCancel={() => voice.answerInstall(false)}
    />
    </>
  );
}
