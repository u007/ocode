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
  if (patch === null) return <div className="p-2 text-xs text-muted-foreground">Loading diff…</div>;

  return (
    <div className="p-2">
      <div className="text-xs text-muted-foreground mb-2 font-mono">{path}</div>
      <div className="font-mono text-xs whitespace-pre-wrap">
        {patch.split("\n").map((line, i) => {
          const lineNo = i + 1;
          let color = "text-muted-foreground";
          if (line.startsWith("+") && !line.startsWith("+++")) color = "text-green-400";
          else if (line.startsWith("-") && !line.startsWith("---")) color = "text-red-400";
          else if (line.startsWith("@@")) color = "text-blue-400";
          return (
            <div key={i} className={`flex ${color}`}>
              <span className="select-none text-neutral-600 w-8 text-right pr-2 shrink-0 text-[10px] leading-4">{lineNo}</span>
              <span className="whitespace-pre-wrap break-words">{line}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
