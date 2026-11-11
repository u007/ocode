import { useEffect, useState } from "react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { eventBus } from "../../lib/eventBus";

interface Account {
  id: string;
  provider: string;
  label: string;
  status: string;
  error?: string;
}

export default function ChatConnectorsForm() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [provider, setProvider] = useState("telegram");
  const [label, setLabel] = useState("");
  const [credentials, setCredentials] = useState("");
  const [busy, setBusy] = useState(false);
  const [syncUrl, setSyncUrl] = useState("");
  const [recent, setRecent] = useState<any[]>([]);
  const [qrMap, setQrMap] = useState<Record<string, string>>({});

  const load = async () => {
    const res = await fetch("/api/messaging/accounts");
    if (res.ok) {
      const accs = await res.json();
      setAccounts(accs);
      for (const a of accs) {
        if (String(a.status || "").startsWith("awaiting_qr")) {
          fetch(`/api/messaging/accounts/${a.id}/qr`)
            .then((r) => r.json())
            .then((d) => { if (d.qr) setQrMap((m) => ({ ...m, [a.id]: d.qr })); })
            .catch(() => {});
        }
      }
    }
  };

  useEffect(() => {
    load();
    const off = eventBus.on("messaging_message", (env) => {
      setRecent((r) => [env.data, ...r].slice(0, 20));
    });
    return () => off();
  }, []);

  const add = async () => {
    setBusy(true);
    try {
      await fetch("/api/messaging/accounts", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider, label, credentials }),
      });
      setLabel("");
      setCredentials("");
      await load();
    } finally {
      setBusy(false);
    }
  };

  const start = async (id: string) => {
    await fetch(`/api/messaging/accounts/${id}/start`, { method: "POST" });
    await load();
  };
  const stop = async (id: string) => {
    await fetch(`/api/messaging/accounts/${id}/stop`, { method: "POST" });
    await load();
  };
  const remove = async (id: string) => {
    await fetch(`/api/messaging/accounts/${id}`, { method: "DELETE" });
    await load();
  };

  return (
    <div className="space-y-4">
      <h3 className="text-lg font-medium">Chat Connectors (WhatsApp / Telegram)</h3>
      {recent.length > 0 && (
        <div className="border rounded p-2 space-y-1 max-h-40 overflow-auto">
          <div className="text-xs font-medium">Recent inbound messages</div>
          {recent.map((m, i) => (
            <div key={i} className="text-xs text-muted-foreground">
              {m.account_id} / {m.chat_id}: {m.text}
            </div>
          ))}
        </div>
      )}
      <div className="space-y-2">
        {accounts.map((a) => (
          <div key={a.id} className="flex items-center gap-2 border rounded p-2">
            <span className="font-mono text-sm">{a.provider}</span>
            <span>{a.label || a.id}</span>
            <span className="text-xs text-muted-foreground">{a.status}{a.error ? " - " + a.error : ""}</span>
            {qrMap[a.id] && <img src={qrMap[a.id]} alt="WhatsApp QR" className="w-32 h-32" />}
            <div className="ml-auto flex gap-1">
              <Button size="sm" onClick={() => start(a.id)}>Start</Button>
              <Button size="sm" variant="outline" onClick={() => stop(a.id)}>Stop</Button>
              <Button size="sm" variant="destructive" onClick={() => remove(a.id)}>Remove</Button>
            </div>
          </div>
        ))}
        {accounts.length === 0 && <p className="text-sm text-muted-foreground">No accounts configured.</p>}
      </div>
      <div className="space-y-2 border rounded p-3">
        <select className="border rounded px-2 py-1" value={provider} onChange={(e) => setProvider(e.target.value)}>
          <option value="telegram">Telegram (bot token)</option>
          <option value="whatsapp">WhatsApp (QR pairing)</option>
        </select>
        <Input placeholder="Label" value={label} onChange={(e) => setLabel(e.target.value)} />
        <Input placeholder={provider === "telegram" ? "Bot token" : "Session dir (optional)"} value={credentials} onChange={(e) => setCredentials(e.target.value)} />
        <Button onClick={add} disabled={busy}>Add account</Button>
        <div className="flex gap-2 items-center">
          <Input placeholder="kakiit sync URL" value={syncUrl} onChange={(e) => setSyncUrl(e.target.value)} />
          <Button size="sm" onClick={async () => {
            const res = await fetch("/api/messaging/sync", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ url: syncUrl, dir: "push" }) });
            if (!res.ok) { const t = await res.text(); alert("Sync failed: " + t); }
          }}>Sync push</Button>
        </div>
      </div>
    </div>
  );
}
