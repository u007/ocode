import { useEffect, useState } from "react";
import { api } from "@/api/client";

interface Props {
  session?: string;
  path: string;
}

export default function ChangesDiffView({ session, path }: Props) {
  const [patch, setPatch] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setPatch(null);
    setError(null);
    api
      .getChangeDiff(session, path)
      .then((res) => {
        if (!cancelled) setPatch(res.patch);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load diff");
      });
    return () => {
      cancelled = true;
    };
  }, [session, path]);

  if (error) return <div className="p-2 text-xs text-red-400">{error}</div>;
  if (patch === null) return <div className="p-2 text-xs text-zinc-500">Loading diff…</div>;

  return (
    <div className="p-2">
      <div className="text-xs text-zinc-500 mb-2 font-mono">{path}</div>
      <pre className="text-xs font-mono whitespace-pre-wrap">
        {patch.split("\n").map((line, i) => {
          let color = "text-zinc-400";
          if (line.startsWith("+") && !line.startsWith("+++")) color = "text-green-400";
          else if (line.startsWith("-") && !line.startsWith("---")) color = "text-red-400";
          else if (line.startsWith("@@")) color = "text-blue-400";
          return (
            <div key={i} className={color}>
              {line}
            </div>
          );
        })}
      </pre>
    </div>
  );
}
