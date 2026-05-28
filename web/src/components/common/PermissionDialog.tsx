import { useState } from "react";

interface Props {
  tool: string;
  command?: string;
  requestId: string;
  onApprove: (requestId: string, approved: boolean) => void;
}

export default function PermissionDialog({ tool, command, requestId, onApprove }: Props) {
  const [dismissed, setDismissed] = useState(false);

  if (dismissed) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="w-full max-w-md rounded-lg border border-zinc-700 bg-zinc-900 p-6 shadow-xl">
        <h3 className="text-lg font-semibold text-zinc-100">Permission Required</h3>
        <p className="mt-2 text-sm text-zinc-400">
          The agent wants to use <span className="font-mono text-blue-400">{tool}</span>
        </p>
        {command && (
          <pre className="mt-3 rounded bg-zinc-800 p-3 text-xs text-zinc-300 overflow-x-auto">
            {command}
          </pre>
        )}
        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={() => { onApprove(requestId, false); setDismissed(true); }}
            className="rounded-lg border border-zinc-600 px-4 py-2 text-sm text-zinc-300 hover:bg-zinc-800"
          >
            Deny
          </button>
          <button
            onClick={() => { onApprove(requestId, true); setDismissed(true); }}
            className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
          >
            Approve
          </button>
        </div>
      </div>
    </div>
  );
}
