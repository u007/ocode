interface Props {
  title: string;
  note: string;
  data: unknown;
}

export default function OpencodeReadOnlyForm({ title, note, data }: Props) {
  return (
    <div className="p-6 max-w-lg space-y-3">
      <h2 className="text-sm font-semibold text-zinc-200">{title}</h2>
      <div className="text-xs text-zinc-500">{note}</div>
      <pre className="rounded-md border border-zinc-700 bg-zinc-800 p-3 text-xs text-zinc-300 overflow-x-auto">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}
