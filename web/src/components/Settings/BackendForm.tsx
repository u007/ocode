import { useCallback, useEffect, useState } from "react";
import { api, getApiBackendBase, setApiBackendBase } from "../../api/client";
import { eventBus } from "../../lib/eventBus";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";

// Radix Select rejects an empty-string item value, so "same origin" (empty
// backend_url) is represented by this sentinel and mapped back to "" when
// saving.
const SAME_ORIGIN = "__same_origin__";

const OPTIONS = [
  { value: SAME_ORIGIN, label: "Same origin (default)" },
  { value: "http://localhost:4096", label: "http://localhost:4096" },
  { value: "https://hub.mercstudio.com", label: "https://hub.mercstudio.com" },
  { value: "__custom__", label: "Custom…" },
];

function normalizeOption(value: string): string {
  if (!value) return SAME_ORIGIN;
  if (value === "https://hub.mercstudio.com") return value;
  if (OPTIONS.some((o) => o.value === value)) return value;
  return "__custom__";
}

export default function BackendForm() {
  const [backendUrl, setBackendUrl] = useState("");
  const [customUrl, setCustomUrl] = useState("");
  const [selectValue, setSelectValue] = useState(SAME_ORIGIN);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeBase, setActiveBase] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getBackendConfig();
      const url = cfg.backend_url || "";
      setBackendUrl(url);
      const opt = normalizeOption(url);
      setSelectValue(opt);
      if (opt === "__custom__") {
        setCustomUrl(url);
      } else {
        setCustomUrl("");
      }
      setActiveBase(getApiBackendBase());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleSelectChange = (v: string) => {
    setSelectValue(v);
    if (v === "__custom__") {
      // keep customUrl as is
      return;
    }
    setBackendUrl(v === SAME_ORIGIN ? "" : v);
    setError(null);
  };

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const toSave =
        selectValue === "__custom__"
          ? customUrl.trim()
          : selectValue === SAME_ORIGIN
            ? ""
            : backendUrl.trim();
      const res = await api.setBackendConfig(toSave);
      const normalized = res.backend_url || "";
      setBackendUrl(normalized);
      setCustomUrl(selectValue === "__custom__" ? normalized : "");
      setApiBackendBase(normalized || null);
      setActiveBase(getApiBackendBase());
      eventBus.restart();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <div>
        <h2 className="text-sm font-semibold text-foreground">Backend</h2>
        <p className="text-xs text-muted-foreground mt-1">
          Choose where the web UI connects for API and events. Empty means same-origin (default).
          Supports <code>http://localhost[:port]</code>, <code>http://127.0.0.1[:port]</code>, and{" "}
          <code>https://hub.mercstudio.com</code>.
        </p>
      </div>
      {error && <div className="text-xs text-red-400 whitespace-pre-wrap">{error}</div>}

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Backend URL</label>
        <Select value={selectValue} onValueChange={handleSelectChange}>
          <SelectTrigger className="h-8 text-xs">
            <SelectValue placeholder="Select backend" />
          </SelectTrigger>
          <SelectContent>
            {OPTIONS.map((opt) => (
              <SelectItem key={opt.value} value={opt.value} className="text-xs">
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {selectValue === "__custom__" && (
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Custom URL</label>
          <Input
            value={customUrl}
            onChange={(e) => setCustomUrl(e.target.value)}
            placeholder="http://localhost:4096 or https://hub.mercstudio.com"
            className="h-8 text-xs"
          />
          <p className="text-[11px] text-muted-foreground">
            Allowed: empty, http://localhost[:port], http://127.0.0.1[:port], https://hub.mercstudio.com
          </p>
        </div>
      )}

      <div className="flex items-center gap-2">
        <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
          {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
          Save
        </Button>
        <Button size="sm" variant="outline" onClick={load} disabled={saving || loading} className="h-8 text-xs">
          Refresh
        </Button>
      </div>

      <div className="text-[11px] text-muted-foreground border-t border-border pt-3 space-y-1">
        <div>
          Configured: <code className="text-foreground">{backendUrl || "(same-origin)"}</code>
        </div>
        <div>
          Active base: <code className="text-foreground">{activeBase || "(same-origin)"}</code>
        </div>
        <p>New API calls use the selected origin immediately; the live events stream reconnects automatically.</p>
        <p>Existing terminal sessions stay on their previous backend (PTY state is host-local and would be lost on switch). New terminals and a page reload use the new backend.</p>
      </div>
    </div>
  );
}
