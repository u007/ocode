import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Paperclip,
  Download,
  Trash2,
  Copy,
  File as FileIcon,
  FileText,
  Image as ImageIcon,
  AlignLeft,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { apiPath, authHeaders } from "@/api/client";

interface UploadedFile {
  name: string;
  size: number;
  modtime: string;
  mime: string;
}

function formatBytes(n: number): string {
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
  if (n < 1073741824) return (n / 1048576).toFixed(1) + " MB";
  return (n / 1073741824).toFixed(1) + " GB";
}

function formatRelTime(iso: string): string {
  const diff = (Date.now() - new Date(iso).getTime()) / 1000;
  if (diff < 60) return "just now";
  if (diff < 3600) return Math.floor(diff / 60) + "m ago";
  if (diff < 86400) return Math.floor(diff / 3600) + "h ago";
  if (diff < 172800) return "yesterday";
  return new Date(iso).toLocaleDateString();
}

function fileIcon(mime: string) {
  if (mime.startsWith("image/")) return ImageIcon;
  if (mime === "application/pdf") return FileText;
  if (mime.startsWith("text/")) return AlignLeft;
  return FileIcon;
}

function fileAPIURL(name: string): string {
  return apiPath(`/api/uploads/file?name=${encodeURIComponent(name)}`);
}

async function fetchBlob(name: string): Promise<string> {
  const r = await fetch(fileAPIURL(name), { headers: authHeaders() });
  if (!r.ok) throw new Error(`fetch failed: ${r.status}`);
  return URL.createObjectURL(await r.blob());
}

async function triggerDownload(name: string): Promise<void> {
  try {
    const url = await fetchBlob(name);
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    console.error("download failed:", e);
  }
}

async function copyToClipboard(text: string): Promise<void> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
  } catch {
    // fall through to the legacy path
  }
  // Fallback for older browsers / non-secure contexts.
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "absolute";
  ta.style.left = "-9999px";
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
  } finally {
    document.body.removeChild(ta);
  }
}

export default function AssetsPanel() {
  const [files, setFiles] = useState<UploadedFile[]>([]);
  const [selected, setSelected] = useState<UploadedFile | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [hoveredFile, setHoveredFile] = useState<string | null>(null);
  const [previewText, setPreviewText] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [blobUrl, setBlobUrl] = useState<string | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);

  // Revoke stale blob URL whenever it changes to avoid memory leaks.
  useEffect(() => {
    return () => {
      if (blobUrl) URL.revokeObjectURL(blobUrl);
    };
  }, [blobUrl]);

  const loadFiles = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const r = await fetch(apiPath("/api/uploads"), {
        headers: authHeaders(),
      });
      if (!r.ok) {
        throw new Error(`list failed: ${r.status} ${r.statusText}`);
      }
      const data: UploadedFile[] = await r.json();
      setFiles(data);
    } catch (e) {
      console.error("Failed to load uploads:", e);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  const uploadFiles = useCallback(
    async (fileList: FileList) => {
      const fd = new FormData();
      Array.from(fileList).forEach((f) => fd.append("file", f));
      try {
        const r = await fetch(apiPath("/api/uploads"), {
          method: "POST",
          headers: authHeaders(),
          body: fd,
        });
        if (!r.ok) {
          throw new Error(`upload failed: ${r.status} ${r.statusText}`);
        }
        await loadFiles();
      } catch (e) {
        console.error("Upload failed:", e);
        setError(e instanceof Error ? e.message : String(e));
      }
    },
    [loadFiles]
  );

  const deleteFile = useCallback(
    async (name: string) => {
      try {
        const r = await fetch(
          apiPath(`/api/uploads?name=${encodeURIComponent(name)}`),
          { method: "DELETE", headers: authHeaders() }
        );
        if (!r.ok && r.status !== 204) {
          throw new Error(`delete failed: ${r.status} ${r.statusText}`);
        }
        setFiles((prev) => prev.filter((f) => f.name !== name));
        setSelected((curr) => (curr?.name === name ? null : curr));
        setConfirmDelete(null);
      } catch (e) {
        console.error("Delete failed:", e);
        setError(e instanceof Error ? e.message : String(e));
      }
    },
    []
  );

  const handleSelect = useCallback(async (file: UploadedFile) => {
    setSelected(file);
    setConfirmDelete(null);
    setPreviewText(null);
    setBlobUrl(null);
    if (file.mime.startsWith("text/")) {
      try {
        const r = await fetch(fileAPIURL(file.name), { headers: authHeaders() });
        setPreviewText(r.ok ? await r.text() : `(failed to load preview: ${r.status})`);
      } catch (e) {
        console.error("Preview load failed:", e);
        setPreviewText(`(failed to load preview: ${e instanceof Error ? e.message : String(e)})`);
      }
    } else {
      try {
        setBlobUrl(await fetchBlob(file.name));
      } catch (e) {
        console.error("Blob load failed:", e);
      }
    }
  }, []);

  const SelectedIcon = useMemo(
    () => (selected ? fileIcon(selected.mime) : FileIcon),
    [selected]
  );

  return (
    <div className="flex h-full bg-zinc-950 text-zinc-100">
      {/* Left pane: file list */}
      <div className="w-64 shrink-0 border-r border-zinc-700 flex flex-col">
        <div className="p-3 border-b border-zinc-700 flex items-center justify-between">
          <span className="text-xs text-zinc-500 uppercase tracking-wider">
            Assets
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={() => fileInputRef.current?.click()}
            title="Upload files"
            aria-label="Upload files"
          >
            <Paperclip className="w-4 h-4" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {loading && files.length === 0 ? (
            <div className="p-3 text-xs text-zinc-500">Loading…</div>
          ) : error && files.length === 0 ? (
            <div className="p-3 text-xs text-red-400">Error: {error}</div>
          ) : files.length === 0 ? (
            <div className="p-3 text-xs text-zinc-500">No files yet.</div>
          ) : (
            files.map((file) => {
              const Icon = fileIcon(file.mime);
              if (confirmDelete === file.name) {
                return (
                  <div
                    key={file.name}
                    className="p-2 text-xs flex items-center justify-between gap-2 bg-zinc-800"
                  >
                    <span className="text-zinc-300 truncate">
                      Delete {file.name}?
                    </span>
                    <div className="flex items-center gap-1 shrink-0">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-6 px-2 text-red-400 hover:text-red-300"
                        onClick={() => deleteFile(file.name)}
                      >
                        Yes
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-6 px-2"
                        onClick={() => setConfirmDelete(null)}
                      >
                        No
                      </Button>
                    </div>
                  </div>
                );
              }
              const isSelected = selected?.name === file.name;
              const isHovered = hoveredFile === file.name;
              return (
                <div
                  key={file.name}
                  className={`p-2 flex items-center gap-2 cursor-pointer rounded ${
                    isSelected ? "bg-zinc-800" : "hover:bg-zinc-800"
                  }`}
                  onClick={() => handleSelect(file)}
                  onMouseEnter={() => setHoveredFile(file.name)}
                  onMouseLeave={() => setHoveredFile(null)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      void handleSelect(file);
                    }
                  }}
                >
                  <Icon className="w-4 h-4 text-zinc-500 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm text-zinc-300 truncate">{file.name}</p>
                    <p className="text-xs text-zinc-500">
                      {formatBytes(file.size)} · {formatRelTime(file.modtime)}
                    </p>
                  </div>
                  {isHovered && (
                    <div className="flex items-center gap-1 shrink-0">
                      <button
                        type="button"
                        onClick={(e) => { e.stopPropagation(); void triggerDownload(file.name); }}
                        title="Download"
                        className="text-zinc-400 hover:text-zinc-200 p-1"
                      >
                        <Download className="w-3.5 h-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          setConfirmDelete(file.name);
                        }}
                        title="Delete"
                        className="text-red-400 hover:text-red-300 p-1"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>

        <div
          className="p-3 border-t border-dashed border-zinc-700 text-center text-xs text-zinc-500 cursor-pointer hover:text-zinc-400"
          onClick={() => fileInputRef.current?.click()}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
              void uploadFiles(e.dataTransfer.files);
            }
          }}
        >
          Drop files or click to upload
        </div>

        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            if (e.target.files && e.target.files.length > 0) {
              void uploadFiles(e.target.files);
              e.target.value = "";
            }
          }}
        />
      </div>

      {/* Right pane: preview */}
      <div className="flex-1 overflow-hidden flex flex-col">
        {!selected ? (
          <div className="flex-1 flex items-center justify-center">
            <p className="text-zinc-500 text-sm">
              Select a file to preview
            </p>
          </div>
        ) : (
          <>
            <div className="p-3 border-b border-zinc-700 space-y-1">
              <div className="flex items-center gap-2">
                <SelectedIcon className="w-4 h-4 text-zinc-500 shrink-0" />
                <p className="text-sm font-medium text-zinc-200 truncate flex-1">
                  {selected.name}
                </p>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2"
                  onClick={() => void triggerDownload(selected.name)}
                >
                  <Download className="w-3.5 h-3.5 mr-1" />
                  Download
                </Button>
              </div>
              <p className="text-xs text-zinc-500">
                {selected.mime || "application/octet-stream"} ·{" "}
                {formatBytes(selected.size)}
              </p>
              <div className="flex items-center gap-2">
                <span className="text-xs text-zinc-600 font-mono flex-1 truncate">
                  /api/uploads/file?name={selected.name}
                </span>
                <button
                  type="button"
                  onClick={() => {
                    void copyToClipboard(
                      `/api/uploads/file?name=${selected.name}`
                    );
                  }}
                  className="text-zinc-500 hover:text-zinc-300 p-1"
                  title="Copy API path"
                >
                  <Copy className="w-3 h-3" />
                </button>
              </div>
            </div>

            <div className="flex-1 overflow-auto p-4 flex items-center justify-center bg-zinc-950">
              {selected.mime.startsWith("image/") ? (
                blobUrl ? (
                  <img
                    src={blobUrl}
                    alt={selected.name}
                    className="max-w-full max-h-full object-contain cursor-zoom-in"
                    onClick={() => setLightboxOpen(true)}
                  />
                ) : (
                  <p className="text-zinc-500 text-sm">Loading…</p>
                )
              ) : selected.mime === "application/pdf" ? (
                blobUrl ? (
                  <iframe
                    src={blobUrl}
                    title={selected.name}
                    className="w-full h-full border-0 bg-zinc-900"
                  />
                ) : (
                  <p className="text-zinc-500 text-sm">Loading…</p>
                )
              ) : selected.mime.startsWith("text/") && previewText !== null ? (
                <pre className="text-xs text-zinc-300 whitespace-pre-wrap overflow-auto w-full h-full">
                  {previewText}
                </pre>
              ) : !selected.mime.startsWith("text/") ? (
                <div className="text-center space-y-2">
                  <p className="text-zinc-500 text-sm">No preview available</p>
                  <button
                    type="button"
                    onClick={() => void triggerDownload(selected.name)}
                    className="inline-flex items-center text-blue-400 hover:text-blue-300 text-sm"
                  >
                    <Download className="w-3.5 h-3.5 mr-1" />
                    Download to view
                  </button>
                </div>
              ) : (
                <p className="text-zinc-500 text-sm">Loading preview…</p>
              )}
            </div>
          </>
        )}
      </div>

      <Dialog
        open={lightboxOpen && !!selected?.mime.startsWith("image/")}
        onOpenChange={setLightboxOpen}
      >
        <DialogContent className="max-w-screen-lg w-[90vw] p-2 bg-zinc-950 border-zinc-800">
          {selected && blobUrl && (
            <img
              src={blobUrl}
              alt={selected.name}
              className="w-full h-auto"
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
