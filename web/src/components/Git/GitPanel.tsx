import { useState, useEffect, useCallback } from "react";
import { ExternalLink } from "lucide-react";
import { api, apiPath, authHeaders } from "@/api/client";
import type { GitDiffFile } from "@/api/types";

interface GitStatus {
  branch: string;
  staged_files: string[];
  changed_files: string[];
  has_changes: boolean;
}

const STATUS_BADGES: Record<string, { label: string; color: string }> = {
  modified: { label: "M", color: "bg-yellow-500/20 text-yellow-400" },
  added: { label: "A", color: "bg-green-500/20 text-green-400" },
  deleted: { label: "D", color: "bg-red-500/20 text-red-400" },
  renamed: { label: "R", color: "bg-blue-500/20 text-blue-400" },
  untracked: { label: "?", color: "bg-zinc-500/20 text-zinc-400" },
};

const REFRESH_INTERVAL = 10000;

interface Props {
  onOpenFile?: (path: string) => void;
}

export default function GitPanel({ onOpenFile }: Props) {
  const [status, setStatus] = useState<GitStatus | null>(null);
  const [files, setFiles] = useState<GitDiffFile[]>([]);
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch(apiPath("/api/git/status"), { headers: authHeaders() });
      const data = await res.json();
      setStatus(data);
    } catch (err) {
      console.error("Failed to fetch git status:", err);
    }
  }, []);

  const fetchDiff = useCallback(async () => {
    try {
      setLoading(true);
      const diffFiles = await api.getGitDiff();
      setFiles(diffFiles);
    } catch (err) {
      console.error("Failed to fetch git diff:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    fetchDiff();
    const interval = setInterval(() => {
      fetchStatus();
      fetchDiff();
    }, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, [fetchStatus, fetchDiff]);

  if (!status) return null;

  const selectedDiff = files.find((f) => f.path === selectedFile);

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-3 border-b border-zinc-700">
        <div className="flex items-center justify-between">
          <label className="text-xs text-zinc-500 uppercase tracking-wider">
            Git
          </label>
          <span className="text-xs text-zinc-400 font-mono">
            {status.branch}
          </span>
        </div>
        {status.has_changes && (
          <div className="mt-1 text-xs text-zinc-500">
            {status.staged_files.length} staged, {status.changed_files.length}{" "}
            changed
          </div>
        )}
      </div>

      {/* File list */}
      <div className="flex-1 overflow-y-auto">
        {loading && files.length === 0 ? (
          <div className="p-3 text-xs text-zinc-500">Loading diff…</div>
        ) : files.length === 0 ? (
          <div className="p-3 text-xs text-zinc-600">No changes</div>
        ) : (
          <div className="divide-y divide-zinc-800">
            {files.map((file) => {
              const badge = STATUS_BADGES[file.status] || STATUS_BADGES.modified;
              const isSelected = selectedFile === file.path;
              return (
                <button
                  key={file.path}
                  onClick={() =>
                    setSelectedFile(isSelected ? null : file.path)
                  }
                  className={`w-full text-left px-3 py-1.5 flex items-center gap-2 text-sm hover:bg-zinc-800/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 ${
                    isSelected ? "bg-zinc-800" : ""
                  }`}
                >
                  <span
                    className={`inline-flex items-center justify-center w-5 h-5 rounded text-[10px] font-bold ${badge.color}`}
                  >
                    {badge.label}
                  </span>
                  <span className="font-mono text-zinc-300 truncate">
                    {file.path}
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Diff view */}
      {selectedDiff && (
        <div className="border-t border-zinc-700 max-h-[40vh] overflow-y-auto">
          <div className="p-2">
            <div className="text-xs text-zinc-500 mb-2 font-mono flex items-center justify-between">
              <span>{selectedDiff.path}</span>
              {onOpenFile && (
                <button
                  onClick={() => onOpenFile(selectedDiff.path)}
                  className="flex items-center gap-1 text-blue-400 hover:text-blue-300 transition-colors"
                  title="Open file in editor tab"
                >
                  <ExternalLink className="w-3 h-3" />
                  <span>Open</span>
                </button>
              )}
            </div>
            <pre className="text-xs font-mono whitespace-pre-wrap">
              {selectedDiff.patch.split("\n").map((line, i) => {
                let color = "text-zinc-400";
                if (line.startsWith("+") && !line.startsWith("+++"))
                  color = "text-green-400";
                else if (line.startsWith("-") && !line.startsWith("---"))
                  color = "text-red-400";
                else if (line.startsWith("@@")) color = "text-blue-400";
                return (
                  <div key={i} className={color}>
                    {line}
                  </div>
                );
              })}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}
