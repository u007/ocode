import { useState } from "react";
import { api } from "../../api/client";

/**
 * Legacy Office (.doc/.ppt) fallback pane. No browser renderer exists
 * (docx-preview handles only .docx; there is no server-side conversion), so
 * both the sidebar PreviewHost and the Files tab land here instead of a
 * confusing "Binary File" editor dead end. "Open in app" runs on the SERVER
 * host (`POST /api/files/open` has no remote branch), so the button is hidden
 * for a remote project — showing it would open an unrelated server-local file.
 */
export default function LegacyOfficePane({
  path,
  projectRoot,
  projectHost,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
}) {
  const [osOpenState, setOsOpenState] = useState<string | null>(null);

  const openWithOS = async () => {
    if (projectHost) return;
    setOsOpenState("Opening…");
    try {
      await api.openFileWithOS(path, projectRoot);
      setOsOpenState("Opened in OS app");
    } catch (e) {
      setOsOpenState(e instanceof Error ? e.message : String(e));
    } finally {
      setTimeout(() => setOsOpenState(null), 2500);
    }
  };

  return (
    <div className="flex h-full flex-1 flex-col items-center justify-center gap-2 p-4 text-center">
      <div className="max-w-[240px] truncate font-mono text-xs text-foreground" title={path}>
        {path}
      </div>
      <div className="max-w-[240px] text-[11px] leading-relaxed text-muted-foreground">
        Legacy Office formats (.doc/.ppt) can't preview in the browser — open with the OS app instead.
      </div>
      {osOpenState && <div className="text-[11px] text-muted-foreground">{osOpenState}</div>}
      {!projectHost && (
        <button
          type="button"
          onClick={openWithOS}
          className="rounded bg-primary px-2 py-1 text-xs text-primary-foreground hover:opacity-90"
        >
          Open in app
        </button>
      )}
    </div>
  );
}
