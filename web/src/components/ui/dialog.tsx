import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Selects text-entry fields only — checkboxes, radios, file/range/color
 * pickers and push-button inputs are not text entry, so they must not win
 * the initial focus (a dialog whose only "inputs" are a dropdown or a
 * checkbox falls through to the annotated default action instead).
 */
const TEXT_INPUT_SELECTOR = [
  'input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"]):not([type="button"]):not([type="submit"]):not([type="reset"]):not([type="image"]):not([type="file"]):not([type="range"]):not([type="color"])',
  "textarea",
  '[contenteditable="true"]',
  '[contenteditable=""]',
].join(", ");

/**
 * Initial-focus policy for every dialog in the app (web + desktop):
 * 1. the first text-entry field in DOM order (search / filter / prompt input,
 *    including cmdk's `[cmdk-input]`), else
 * 2. the button annotated `data-dialog-default-action` (the safe primary
 *    action, e.g. "Allow once" — never the destructive first button), else
 * 3. Radix's own default: the first tabbable element.
 * Returns true when a target was focused so the caller can preventDefault
 * Radix's "focus first tabbable" pass.
 */
function focusDialogInitialTarget(content: HTMLElement | null): boolean {
  if (!content) return false;
  const inputs = content.querySelectorAll<HTMLElement>(TEXT_INPUT_SELECTOR);
  for (const el of Array.from(inputs)) {
    // focus() is a silent no-op on disabled/hidden elements; the
    // activeElement check moves on to the next candidate in that case.
    el.focus({ preventScroll: true });
    if (document.activeElement === el) return true;
  }
  const defaultAction = content.querySelector<HTMLElement>(
    "[data-dialog-default-action]:not([disabled])",
  );
  if (defaultAction) {
    defaultAction.focus({ preventScroll: true });
    if (document.activeElement === defaultAction) return true;
  }
  return false;
}

const Dialog = DialogPrimitive.Root;

const DialogTrigger = DialogPrimitive.Trigger;

const DialogPortal = DialogPrimitive.Portal;

const DialogClose = DialogPrimitive.Close;

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/80 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => {
  // Ref that observes the dialog content as it mounts so the open-focus
  // callback can reach the (forwarded) DOM node.
  const contentRef = React.useRef<HTMLDivElement | null>(null);
  const setRef = (node: HTMLDivElement | null) => {
    contentRef.current = node;
    if (typeof ref === "function") ref(node);
    else if (ref) (ref as React.MutableRefObject<HTMLDivElement | null>).current = node;
  };
  return (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={setRef}
      onOpenAutoFocus={(e) => {
        // Runs after mount (portal children are committed before Radix
        // dispatches the auto-focus event), so the first input / annotated
        // default action is in the DOM. Callers can still override with
        // their own onOpenAutoFocus (passed props win — this is the default).
        if (focusDialogInitialTarget(contentRef.current)) e.preventDefault();
      }}
      className={cn(
        "fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 border bg-background p-6 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-[48%] data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-[48%] sm:rounded-lg",
        className
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground">
        <X className="h-4 w-4" />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
  );
});
DialogContent.displayName = DialogPrimitive.Content.displayName;

const DialogHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col space-y-1.5 text-center sm:text-left",
      className
    )}
    {...props}
  />
);
DialogHeader.displayName = "DialogHeader";

const DialogFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2",
      className
    )}
    {...props}
  />
);
DialogFooter.displayName = "DialogFooter";

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn(
      "text-lg font-semibold leading-none tracking-tight",
      className
    )}
    {...props}
  />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogClose,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
};
