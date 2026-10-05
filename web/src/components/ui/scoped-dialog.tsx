import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";
import { focusDialogInitialTarget } from "./dialog-focus";
import {
  DialogClose,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./dialog";

/**
 * A shadcn-style dialog confined to ONE target element instead of the
 * viewport: the dimmed backdrop, the dialog panel and the click-to-dismiss
 * area all live inside `container`, so the rest of the app stays visible and
 * usable. Use it for a confirm inside a panel (a preview pane, a sidebar
 * section, a split view) where a viewport-level modal would black out the
 * whole screen.
 *
 * Three Radix behaviours are deliberately NOT the shadcn defaults here, all
 * because `modal={false}` is what keeps the rest of the app interactive:
 *
 * 1. `modal={false}` disables `hideOthers()`, so the app is never `aria-hidden`
 *    behind the dialog, and never gets `body { pointer-events: none }`. It
 *    also means `DialogPrimitive.Overlay` renders nothing at all (radix
 *    `index.mjs:102`), so the scrim here is a plain element.
 * 2. It sets the FocusScope's `trapped` to false, so focus is NOT re-claimed
 *    when it leaves the panel — which is what lets the surrounding app keep
 *    being used while this dialog is open. Tab still wraps at the panel's
 *    edges because Radix passes `loop: true` unconditionally (its
 *    `handleKeyDown` bails only on `!loop && !trapped`); do not hand-roll a
 *    second Tab trap on top, it is both redundant and already proven wrong.
 * 3. A non-modal layer dismisses on ANY pointerdown or focus move that lands
 *    outside its content — including a click on an unrelated part of the app.
 *    Every "outside" handler therefore ignores interactions outside `container`,
 *    which keeps the rest of the app clickable without dismissing the dialog.
 *
 * `ScopedDialogHeader` / `Footer` / `Title` / `Description` / `Trigger` /
 * `Close` are the plain `dialog.tsx` parts re-exported: they carry no
 * modal/portal behaviour, so sharing them avoids a second copy to drift.
 *
 * Complementary to `web/src/lib/dialogScope.ts`, not a replacement: that
 * predicate decides whether a session-bound dialog may be MOUNTED at all (an
 * ask for an off-screen session must not block the app); this component
 * decides WHERE a dialog that legitimately should show is rendered.
 */

/** The element the dialog is confined to, or a ref to it. */
export type ScopedDialogContainer =
  | HTMLElement
  | null
  | React.RefObject<HTMLElement | null>;

/** Derived from Radix's own prop types so they cannot drift out of sync. */
type ContentProps = React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>;
type PointerDownOutsideHandler = NonNullable<ContentProps["onPointerDownOutside"]>;
type FocusOutsideHandler = NonNullable<ContentProps["onFocusOutside"]>;
type InteractOutsideHandler = NonNullable<ContentProps["onInteractOutside"]>;

/**
 * A ref is indistinguishable from an element by type alone, so probe for the
 * `current` field. `HTMLElement` has no `current`, so this is unambiguous.
 */
function resolveContainer(container: ScopedDialogContainer): HTMLElement | null {
  if (container && typeof container === "object" && "current" in container) {
    return container.current ?? null;
  }
  return container ?? null;
}

const ScopedDialogContainerContext = React.createContext<HTMLElement | null>(null);

export interface ScopedDialogProps
  extends Omit<React.ComponentPropsWithoutRef<typeof DialogPrimitive.Root>, "modal"> {
  /**
   * The element to confine the dialog to. It must establish a positioning
   * context (`position: relative` / `absolute` / `fixed`), because the scrim
   * and the panel are positioned `absolute` against it.
   *
   * Prefer passing an element held in STATE, set from a callback ref:
   *
   *     const [target, setTarget] = React.useState<HTMLDivElement | null>(null);
   *     <div ref={setTarget}>…</div>
   *     <ScopedDialog container={target} … />
   *
   * A `useRef` is accepted too, but `ref.current` is still null during the
   * first render, so the dialog stays hidden until something else re-renders
   * the owner. That is why an open-but-unresolved container warns below.
   */
  container: ScopedDialogContainer;
}

function ScopedDialog({ container, children, ...props }: ScopedDialogProps) {
  const resolved = resolveContainer(container);
  // Mirrored into state so a ref that fills in later (or a container that
  // changes) re-mounts the portal into the right element.
  const [target, setTarget] = React.useState<HTMLElement | null>(resolved);
  React.useEffect(() => {
    setTarget(resolveContainer(container));
  });

  // `position: static` would make the `absolute` scrim and panel resolve
  // against some distant positioned ancestor — i.e. silently cover the whole
  // app, the exact failure this component exists to prevent. Fix it in place
  // and restore the inline value on cleanup, and say so in dev.
  React.useEffect(() => {
    if (!target || typeof window === "undefined") return;
    // "" is what a non-browser DOM (jsdom) reports for an unset position;
    // both it and "static" mean the container establishes no positioning
    // context, so both need the fix.
    const position = window.getComputedStyle(target).position;
    if (position !== "" && position !== "static") return;
    const previous = target.style.position;
    target.style.position = "relative";
    if (import.meta.env?.DEV) {
      console.warn(
        "[ScopedDialog] container had position:static; set position:relative on it " +
          "so the scrim and panel stay confined to it",
        target,
      );
    }
    return () => {
      target.style.position = previous;
    };
  }, [target]);

  // An open dialog with nowhere to render is invisible and unexplainable, so
  // say it once — but only if it is STILL unresolved. The documented state
  // form resolves on the SECOND render (the ref callback fires during commit,
  // before effects), so checking synchronously warned on correct usage too,
  // and a diagnostic that cries wolf gets ignored. The timer is cleared by
  // this effect's cleanup as soon as `target` arrives.
  const warned = React.useRef(false);
  React.useEffect(() => {
    if (target || warned.current || !props.open) return;
    const id = window.setTimeout(() => {
      if (warned.current) return;
      warned.current = true;
      console.warn(
        "[ScopedDialog] open with an unresolved container — pass an element held in " +
          "state (ref={setTarget}) rather than a ref's .current",
        { container },
      );
    }, 0);
    return () => window.clearTimeout(id);
  }, [target, props.open, container]);

  return (
    <ScopedDialogContainerContext.Provider value={target}>
      <DialogPrimitive.Root modal={false} {...props}>
        {children}
      </DialogPrimitive.Root>
    </ScopedDialogContainerContext.Provider>
  );
}
ScopedDialog.displayName = "ScopedDialog";

const ScopedDialogTrigger = DialogTrigger;

/**
 * The dimmed backdrop, confined to the container. NOT `DialogPrimitive.Overlay`:
 * that renders nothing while `modal={false}`.
 */
const ScopedDialogOverlay = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    data-scoped-dialog-overlay=""
    aria-hidden="true"
    className={cn(
      "absolute inset-0 z-40 bg-black/50 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className,
    )}
    {...props}
  />
));
ScopedDialogOverlay.displayName = "ScopedDialogOverlay";

export interface ScopedDialogContentProps extends ContentProps {
  /** Class for the dimmed backdrop. `null` omits it entirely. */
  overlayClassName?: string | null;
}

const ScopedDialogContent = React.forwardRef<
  HTMLDivElement,
  ScopedDialogContentProps
>(
  (
    {
      className,
      children,
      overlayClassName,
      tabIndex = -1,
      onPointerDownOutside,
      onFocusOutside,
      onInteractOutside,
      onOpenAutoFocus,
      ...props
    },
    ref,
  ) => {
    const container = React.useContext(ScopedDialogContainerContext);
    // Observes the panel as it mounts so the open-focus callback can reach the
    // (forwarded) DOM node.
    const contentRef = React.useRef<HTMLDivElement | null>(null);
    const setRef = (node: HTMLDivElement | null) => {
      contentRef.current = node;
      if (typeof ref === "function") ref(node);
      else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = node;
    };

    /**
     * A non-modal layer dismisses on any interaction outside its content.
     * Outside the CONTAINER is a different app region the user meant to use,
     * so those interactions must not close the dialog. Radix gates dismissal
     * on `defaultPrevented`, and hands the SAME event to both handlers.
     */
    const guard =
      <E extends { defaultPrevented: boolean; preventDefault(): void; target: EventTarget | null }>(
        handler: ((event: E) => void) | undefined,
      ) =>
      (event: E) => {
        handler?.(event);
        if (event.defaultPrevented) return;
        const target = event.target as Node | null;
        if (!target || !container?.contains(target)) event.preventDefault();
      };

    return (
      <>
        {/* No container yet means nothing to confine to; rendering here would
            cover the whole viewport, which is the behaviour being avoided. */}
        {container ? (
          <DialogPrimitive.Portal container={container}>
            {overlayClassName === null ? null : <ScopedDialogOverlay className={overlayClassName} />}
            <DialogPrimitive.Content
              ref={setRef}
              tabIndex={tabIndex}
              onOpenAutoFocus={(event) => {
                // Same shared policy as every other dialog: first text entry,
                // else the annotated default action, else Radix's first
                // tabbable. Callers can still override via this prop.
                if (focusDialogInitialTarget(contentRef.current)) event.preventDefault();
                onOpenAutoFocus?.(event);
              }}
              onPointerDownOutside={guard<Parameters<PointerDownOutsideHandler>[0]>(onPointerDownOutside)}
              onFocusOutside={guard<Parameters<FocusOutsideHandler>[0]>(onFocusOutside)}
              onInteractOutside={guard<Parameters<InteractOutsideHandler>[0]>(onInteractOutside)}
              className={cn(
                "absolute left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 overflow-y-auto border bg-background p-6 shadow-lg duration-200 max-h-[calc(100%-2rem)] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 sm:rounded-lg",
                className,
              )}
              {...props}
            >
              {children}
              <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground">
                <X className="h-4 w-4" />
                <span className="sr-only">Close</span>
              </DialogPrimitive.Close>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        ) : null}
      </>
    );
  },
);
ScopedDialogContent.displayName = "ScopedDialogContent";

export {
  ScopedDialog,
  ScopedDialogTrigger,
  ScopedDialogOverlay,
  ScopedDialogContent,
  DialogHeader as ScopedDialogHeader,
  DialogFooter as ScopedDialogFooter,
  DialogTitle as ScopedDialogTitle,
  DialogDescription as ScopedDialogDescription,
  DialogClose as ScopedDialogClose,
};