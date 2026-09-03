import { useEffect, useState, useCallback } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "../ui/dialog";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Copy, Check, Monitor } from "lucide-react";
import { authToken } from "../../api/client";

export default function ShareDialog() {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  // There is exactly one share URL: full desktop access. The server's auth
  // token authorizes every API route (not just one session), so a
  // "session-only" link would silently grant control over all sessions,
  // projects, files, terminals, and configuration. Until scoped capability
  // tokens exist, do not present session-only sharing.
  const desktopUrl = (() => {
    const token = authToken();
    const origin = window.location.origin;
    const base = window.location.pathname.match(/^(.*?)\/session\/[^/]+$/)?.[1] ?? "";
    const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
    return `${origin}${base}/${suffix}`.replace(/\/\//g, "/").replace(":/", "://");
  })();

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(desktopUrl);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback: select input
    }
  }, [desktopUrl]);

  useEffect(() => {
    const openDialog = () => {
      setOpen(true);
      setCopied(false);
    };
    const onShareSession = () => openDialog();
    const onShareDesktop = () => openDialog();
    const onCopyDesktop = async () => {
      try {
        await navigator.clipboard.writeText(desktopUrl);
      } catch {}
    };
    window.addEventListener("ocode:share-session", onShareSession);
    window.addEventListener("ocode:share-desktop", onShareDesktop);
    window.addEventListener("ocode:copy-desktop-url", onCopyDesktop);
    return () => {
      window.removeEventListener("ocode:share-session", onShareSession);
      window.removeEventListener("ocode:share-desktop", onShareDesktop);
      window.removeEventListener("ocode:copy-desktop-url", onCopyDesktop);
    };
  }, [desktopUrl]);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="w-4 h-4" />
            Share Entire Desktop
          </DialogTitle>
          <DialogDescription>
            Anyone with this URL gets full access to this desktop server — all sessions, projects, files, terminals, and settings — because the link carries the server credential. Only share it with people you trust with complete control.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 pt-2">
          <div className="flex gap-2">
            <Input value={desktopUrl} readOnly className="font-mono text-xs flex-1" onFocus={(e) => e.currentTarget.select()} />
            <Button size="sm" onClick={handleCopy} className="shrink-0 gap-1">
              {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>

          <div className="text-[11px] text-muted-foreground border-t pt-3">
            <p>Desktop shell: <code className="bg-muted px-1 py-0.5 rounded">Share</code> menu → <code className="bg-muted px-1 py-0.5 rounded">⌘⇧S</code> for session.</p>
            <p className="mt-1">TUI equivalent: <code className="bg-muted px-1 py-0.5 rounded">/rc [port]</code> / <code className="bg-muted px-1 py-0.5 rounded">/rc off</code> in the chat input.</p>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
