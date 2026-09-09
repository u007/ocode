import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function BrowserForm() {
  const [chromePath, setChromePath] = useState("");
  const [idleTimeoutMinutes, setIdleTimeoutMinutes] = useState(10);
  const [screencastQuality, setScreencastQuality] = useState(85);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getBrowserConfig();
      setChromePath(cfg.chrome_path);
      setIdleTimeoutMinutes(cfg.idle_timeout_minutes);
      setScreencastQuality(cfg.screencast_quality);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const saved = await api.setBrowserConfig({
        chrome_path: chromePath,
        idle_timeout_minutes: idleTimeoutMinutes,
        screencast_quality: screencastQuality,
      });
      setScreencastQuality(saved.screencast_quality);
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
      <h2 className="text-sm font-semibold text-foreground">Browser</h2>
      <p className="text-xs text-muted-foreground">
        Embedded Chrome tab (CDP screencast) settings. Quality applies live — open tabs pick it up on their
        next resize or navigation.
      </p>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">
          Screencast quality ({screencastQuality}) — higher is sharper text, more bandwidth
        </label>
        <input
          type="range"
          min={1}
          max={100}
          value={screencastQuality}
          onChange={(e) => setScreencastQuality(Number(e.target.value))}
          className="w-full"
          data-testid="browser-quality"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Chrome binary path (empty = auto-discover)</label>
        <Input
          type="text"
          value={chromePath}
          onChange={(e) => setChromePath(e.target.value)}
          placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Idle timeout (minutes, 0 = default)</label>
        <Input
          type="number"
          value={idleTimeoutMinutes}
          onChange={(e) => setIdleTimeoutMinutes(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
