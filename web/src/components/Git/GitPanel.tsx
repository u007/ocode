import { useState, useEffect } from "react";

interface GitStatus {
  branch: string;
  staged_files: string[];
  changed_files: string[];
  has_changes: boolean;
}

const REFRESH_INTERVAL = 10000; // 10 seconds

export default function GitPanel() {
  const [status, setStatus] = useState<GitStatus | null>(null);

  const fetchStatus = async () => {
    try {
      const res = await fetch("/api/git/status");
      const data = await res.json();
      setStatus(data);
    } catch (err) {
      console.error("Failed to fetch git status:", err);
    }
  };

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, REFRESH_INTERVAL);
    return () => clearInterval(interval);
  }, []);

  if (!status) return null;

  return (
    <div className="p-3 border-b border-zinc-700">
      <label className="text-xs text-zinc-500 uppercase tracking-wider">Git</label>
      <div className="mt-1 text-sm">
        <div className="text-zinc-300 font-mono">{status.branch}</div>
        {status.has_changes ? (
          <div className="mt-1 text-xs text-zinc-500">
            {status.staged_files.length} staged, {status.changed_files.length} changed
          </div>
        ) : (
          <div className="mt-1 text-xs text-zinc-600">clean</div>
        )}
      </div>
    </div>
  );
}
