import { useEffect, useState } from "react";
import { api } from "@/api/client";
import type { STTEngine, STTSettings } from "@/api/types";

function errorText(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

const ENGINE_LABEL: Record<STTEngine, string> = {
  local: "On this machine",
  openai: "OpenAI",
};

/** Speech-to-text settings: the model picker and the filler-word cleanup
 *  toggle for voice input. The server owns both choices; every change is a PUT
 *  whose response replaces the state, so the form never shows a value the
 *  server did not accept. */
export default function SttForm() {
  const [settings, setSettings] = useState<STTSettings | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .getSTT()
      .then((next) => {
        if (!cancelled) setSettings(next);
      })
      .catch((err) => {
        if (!cancelled) setLoadError(errorText(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // save runs one PUT and adopts its response, so the form never shows a
  // setting the server did not accept.
  const save = async (request: () => Promise<STTSettings>) => {
    if (saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      setSettings(await request());
    } catch (err) {
      setSaveError(errorText(err));
    } finally {
      setSaving(false);
    }
  };

  const select = (id: string) => {
    if (!settings || settings.selected === id) return;
    void save(() => api.setSTTModel(id));
  };

  const toggleFillers = (remove: boolean) => {
    if (!settings || settings.remove_fillers === remove) return;
    void save(() => api.setSTTFillers(remove));
  };

  const selected = settings?.models.find((m) => m.id === settings.selected);

  return (
    <section className="space-y-4 rounded-lg border border-border p-4">
      <div>
        <h2 className="text-sm font-semibold">Speech to text</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Turns a voice message from the composer into text, which is then sent as your message.
          Local models run on this machine; OpenAI sends the recording to OpenAI.
        </p>
      </div>

      {loadError && <p className="text-xs text-destructive">Speech-to-text settings unavailable: {loadError}</p>}
      {!settings && !loadError && <p className="text-xs text-muted-foreground">Loading…</p>}

      {settings && (
        <>
          <p className="text-xs text-muted-foreground">
            Selected model:{" "}
            <span className="font-medium text-foreground">{selected?.label ?? (settings.selected || "none")}</span>
          </p>
          <div role="radiogroup" aria-label="Speech to text model" className="space-y-2">
            {settings.models.map((model) => {
              const checked = model.id === settings.selected;
              return (
                <label
                  key={model.id}
                  data-testid={`stt-model-${model.id}`}
                  className={`flex min-w-0 items-start gap-3 rounded-md border border-border bg-card p-3 text-xs shadow-sm ${
                    model.available ? "cursor-pointer hover:bg-muted/50" : "cursor-not-allowed opacity-60"
                  }`}
                >
                  <input
                    type="radio"
                    name="stt-model"
                    value={model.id}
                    className="mt-0.5 shrink-0"
                    checked={checked}
                    disabled={!model.available}
                    onChange={() => void select(model.id)}
                  />
                  <span className="min-w-0 flex-1 space-y-0.5">
                    <span className="block break-words text-sm font-medium">{model.label}</span>
                    <span className="block break-words text-[11px] text-muted-foreground">
                      {ENGINE_LABEL[model.engine] ?? model.engine}
                      {" · "}
                      {model.languages}
                      {model.size_mb ? ` · ${model.size_mb} MB` : ""}
                    </span>
                    <span className="block break-words text-[11px] text-muted-foreground">{model.description}</span>
                    {!model.available && model.reason && (
                      <span className="block break-words text-[11px] text-destructive">{model.reason}</span>
                    )}
                  </span>
                </label>
              );
            })}
          </div>
          <label
            data-testid="stt-remove-fillers"
            className="flex min-w-0 items-start gap-3 rounded-md border border-border bg-card p-3 text-xs shadow-sm"
          >
            <input
              type="checkbox"
              className="mt-0.5 shrink-0"
              checked={settings.remove_fillers !== false}
              disabled={saving}
              onChange={(e) => toggleFillers(e.target.checked)}
            />
            <span className="min-w-0 flex-1 space-y-0.5">
              <span className="block break-words text-sm font-medium">Remove filler words</span>
              <span className="block break-words text-[11px] text-muted-foreground">
                Drops hesitation sounds such as “um”, “uh” and “hmm” after the transcription. Turn it off to
                keep the transcript exactly as the model heard it.
              </span>
            </span>
          </label>
          {saving && <p className="text-[11px] text-muted-foreground">Saving…</p>}
          {saveError && <p className="text-[11px] text-destructive">{saveError}</p>}
        </>
      )}
    </section>
  );
}
