interface Props {
  title: string;
  note: string;
  data: unknown;
}

export default function OpencodeReadOnlyForm({ title, note, data }: Props) {
  return (
    <div className="p-6 max-w-lg space-y-3">
      <h2 className="text-sm font-semibold text-foreground">{title}</h2>
      <div className="text-xs text-muted-foreground">{note}</div>
      <pre className="rounded-md border border-border bg-muted p-3 text-xs text-foreground overflow-x-auto">
        {JSON.stringify(data, null, 2)}
      </pre>
    </div>
  );
}
